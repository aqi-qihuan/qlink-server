package streamer

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/segmentio/kafka-go"
)

const (
	dwsPVSourceTopic = "dwm_link_visit_topic"
	dwsUVSourceTopic = "dwm_unique_visitor_topic"
	dwsPVGroupID     = "dws_link_visit_group"
	dwsUVGroupID     = "dws_unique_visitor_group"
	windowDuration   = 10 * time.Second
)

// DWSJob reads PV + UV streams, aggregates in 10s windows, writes to ClickHouse.
type DWSJob struct {
	pvReader *kafka.Reader
	uvReader *kafka.Reader
	chConn   clickhouse.Conn
	cancel   context.CancelFunc
}

// pendingBatch holds one flush cycle's aggregate state plus the Kafka messages
// that produced it. Messages are committed ONLY after a successful ClickHouse
// insert. If we commit earlier (as before) a crash between commit and insert
// loses the window permanently; if we re-deliver into an already-merged window
// without stable keys, PV/UV double-count.
type pendingBatch struct {
	stats  map[StatsKey]*VisitStats
	pvMsgs []kafka.Message
	uvMsgs []kafka.Message
}

func newPendingBatch() *pendingBatch {
	return &pendingBatch{stats: make(map[StatsKey]*VisitStats)}
}

func (b *pendingBatch) addMessage(label string, msg kafka.Message) {
	if label == "PV" {
		b.pvMsgs = append(b.pvMsgs, msg)
	} else {
		b.uvMsgs = append(b.uvMsgs, msg)
	}
}

// mergeBatch folds a failed batch back into the current pending batch so the
// next flush retries it instead of dropping it.
func (b *pendingBatch) mergeBatch(failed *pendingBatch) {
	for key, stats := range failed.stats {
		if existing, ok := b.stats[key]; ok {
			existing.PV += stats.PV
			existing.UV += stats.UV
		} else {
			b.stats[key] = stats
		}
	}
	b.pvMsgs = append(b.pvMsgs, failed.pvMsgs...)
	b.uvMsgs = append(b.uvMsgs, failed.uvMsgs...)
}

func NewDWSJob(brokers string, chConn clickhouse.Conn) *DWSJob {
	pvReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(brokers, ","),
		Topic:       dwsPVSourceTopic,
		GroupID:     dwsPVGroupID,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})
	uvReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(brokers, ","),
		Topic:       dwsUVSourceTopic,
		GroupID:     dwsUVGroupID,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})
	return &DWSJob{pvReader: pvReader, uvReader: uvReader, chConn: chConn}
}

func (j *DWSJob) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	j.cancel = cancel
	log.Println("[DWS] Started: consuming PV + UV streams, window =", windowDuration)

	var mu sync.Mutex
	pending := newPendingBatch()

	// PV consumer goroutine
	go j.consumeStream(ctx, j.pvReader, "PV", &mu, pending, func(w ShortLinkWide, stats *VisitStats) {
		stats.PV++
	})

	// UV consumer goroutine
	go j.consumeStream(ctx, j.uvReader, "UV", &mu, pending, func(w ShortLinkWide, stats *VisitStats) {
		stats.UV++
	})

	// Flush ticker every 10 seconds. Window boundaries come from event time
	// (visitTime bucket), not from this ticker — see windowTimeBoundsFromBucket.
	ticker := time.NewTicker(windowDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Use a fresh context with timeout for the final flush, because
			// ctx is already cancelled and both ClickHouse PrepareBatch and
			// Kafka CommitMessages would immediately return context.Canceled.
			flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
			j.flush(flushCtx, &mu, pending)
			flushCancel()
			log.Println("[DWS] Stopped")
			return
		case <-ticker.C:
			j.flush(ctx, &mu, pending)
		}
	}
}

func (j *DWSJob) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
	j.pvReader.Close()
	j.uvReader.Close()
}

// eventWindowBucket truncates a visitTime (ms) to the start of its 10s
// tumbling window. Using it in StatsKey means a redelivered message — same
// visitTime — always maps to the same aggregation group and the same
// ClickHouse ORDER BY key, which is what makes the sink effectively idempotent.
func eventWindowBucket(visitTimeMillis int64, duration time.Duration) int64 {
	d := duration.Milliseconds()
	return (visitTimeMillis / d) * d
}

// windowTimeBoundsFromBucket returns the [start, end] strings stamped on a
// flushed batch, derived from the event-time bucket (not the ticker's wall
// clock). This aligns with Flink's TumblingEventTimeWindows and, crucially,
// makes the ClickHouse ORDER BY key stable across redelivery.
func windowTimeBoundsFromBucket(bucketMillis int64, duration time.Duration) (string, string) {
	start := time.UnixMilli(bucketMillis)
	return FormatDateTime(start), FormatDateTime(start.Add(duration))
}

type enrichFn func(ShortLinkWide, *VisitStats)

func (j *DWSJob) consumeStream(ctx context.Context, reader *kafka.Reader, label string, mu *sync.Mutex, pending *pendingBatch, enrich enrichFn) {
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[DWS-%s] fetch error: %v", label, err)
			time.Sleep(time.Second)
			continue
		}

		wide, err := parseWide(msg.Value)
		if err != nil {
			// Poison message: never aggregates, drop by committing so it
			// cannot block the partition forever.
			log.Printf("[DWS-%s] parse error: %v", label, err)
			reader.CommitMessages(ctx, msg)
			continue
		}

		key := buildStatsKey(wide)
		mu.Lock()
		stats, ok := pending.stats[key]
		if !ok {
			stats = &VisitStats{
				Code:        wide.Code,
				Referer:     wide.Referer,
				IsNew:       wide.IsNew,
				AccountNo:   wide.AccountNo,
				Province:    wide.Province,
				City:        wide.City,
				IP:          wide.IP,
				BrowserName: wide.BrowserName,
				OS:          wide.OS,
				DeviceType:  wide.DeviceType,
				VisitTime:   wide.VisitTime,
			}
			pending.stats[key] = stats
		}
		enrich(wide, stats)
		// Record the message but DO NOT commit here. Offset advances only
		// after the aggregate is durably written by flush().
		pending.addMessage(label, msg)
		mu.Unlock()
	}
}

// detachBatch takes the current pending contents and installs a fresh empty
// batch under the same pointer, so consumer goroutines keep writing to a new
// window while flush owns the old one.
//
// The VALUE COPY is essential. `batch := pending` would alias the same struct,
// and resetting *pending would then also wipe batch.stats — flush would see
// an empty window and silently drop every aggregate. This exact bug shipped
// in the first version of the commit-timing fix (DWS kept logging
// "Flushed 0 records" while consumeStream was adding to stats).
func detachBatch(pending *pendingBatch) pendingBatch {
	batch := *pending
	*pending = pendingBatch{stats: make(map[StatsKey]*VisitStats)}
	return batch
}

func (j *DWSJob) flush(ctx context.Context, mu *sync.Mutex, pending *pendingBatch) {
	mu.Lock()
	if len(pending.stats) == 0 {
		mu.Unlock()
		return
	}

	// Detach the current batch; consumer goroutines keep filling a fresh one.
	batch := detachBatch(pending)
	mu.Unlock()

	records := make([]*VisitStats, 0, len(batch.stats))
	for key, stats := range batch.stats {
		// Window bounds come from the event-time bucket, NOT the ticker clock.
		start, end := windowTimeBoundsFromBucket(key.WindowBucket, windowDuration)
		stats.StartTime = start
		stats.EndTime = end
		records = append(records, stats)
	}

	if err := j.batchInsert(ctx, records); err != nil {
		log.Printf("[DWS] ClickHouse insert error (%d records): %v", len(records), err)
		// Roll back BOTH aggregates and message offsets so the next flush
		// retries from the same Kafka position. Rolling back only the stats
		// (the old behavior) left the offsets committed and lost the data.
		mu.Lock()
		pending.mergeBatch(&batch)
		mu.Unlock()
		return
	}

	// Insert succeeded — now it is safe to advance Kafka offsets.
	j.commitBatch(ctx, &batch)
	log.Printf("[DWS] Flushed %d records to ClickHouse (PV msgs=%d, UV msgs=%d)",
		len(records), len(batch.pvMsgs), len(batch.uvMsgs))
}

// commitBatch advances offsets for a successfully written batch. Each message
// is committed in order; a commit error is logged but does not roll back the
// ClickHouse write — on restart the same event-time key yields the same
// ORDER BY row and ReplacingMergeTree dedupes it, so re-delivery is safe.
func (j *DWSJob) commitBatch(ctx context.Context, batch *pendingBatch) {
	commitMessages(ctx, batch, j.pvReader.CommitMessages, j.uvReader.CommitMessages)
}

// commitMessages is the pure form of commitBatch, taking the two commit
// callbacks so the ordering/count behavior is unit-testable without Kafka.
// commitPV/commitUV have the kafka.Reader.CommitMessages signature.
func commitMessages(
	ctx context.Context,
	batch *pendingBatch,
	commitPV func(context.Context, ...kafka.Message) error,
	commitUV func(context.Context, ...kafka.Message) error,
) {
	for _, msg := range batch.pvMsgs {
		if err := commitPV(ctx, msg); err != nil && ctx.Err() == nil {
			log.Printf("[DWS-PV] commit error: %v", err)
		}
	}
	for _, msg := range batch.uvMsgs {
		if err := commitUV(ctx, msg); err != nil && ctx.Err() == nil {
			log.Printf("[DWS-UV] commit error: %v", err)
		}
	}
}

func (j *DWSJob) batchInsert(ctx context.Context, records []*VisitStats) error {
	batch, err := j.chConn.PrepareBatch(ctx, insertSQL)
	if err != nil {
		return err
	}

	for _, r := range records {
		err := batch.Append(
			r.Code, r.Referer, r.IsNew, r.AccountNo,
			r.Province, r.City, r.IP, r.BrowserName, r.OS, r.DeviceType,
			r.PV, r.UV, r.StartTime, r.EndTime, r.VisitTime,
		)
		if err != nil {
			log.Printf("[DWS] append error: %v", err)
			continue
		}
	}

	return batch.Send()
}

func parseWide(raw []byte) (ShortLinkWide, error) {
	var w ShortLinkWide
	err := json.Unmarshal(raw, &w)
	return w, err
}

func buildStatsKey(w ShortLinkWide) StatsKey {
	return StatsKey{
		Code:         w.Code,
		Referer:      w.Referer,
		IsNew:        w.IsNew,
		Province:     w.Province,
		City:         w.City,
		IP:           w.IP,
		BrowserName:  w.BrowserName,
		OS:           w.OS,
		DeviceType:   w.DeviceType,
		WindowBucket: eventWindowBucket(w.VisitTime, windowDuration),
	}
}
