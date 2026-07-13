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
	window := make(map[StatsKey]*VisitStats)

	// PV consumer goroutine
	go j.consumeStream(ctx, j.pvReader, "PV", &mu, &window, func(w ShortLinkWide, stats *VisitStats) {
		stats.PV++
	})

	// UV consumer goroutine
	go j.consumeStream(ctx, j.uvReader, "UV", &mu, &window, func(w ShortLinkWide, stats *VisitStats) {
		stats.UV++
	})

	// Window ticker: flush every 10 seconds (processing-time window)
	ticker := time.NewTicker(windowDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Use a fresh context with timeout for the final flush, because
			// ctx is already cancelled and ClickHouse PrepareBatch would
			// immediately return context.Canceled, losing the last window's data.
			flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
			j.flush(flushCtx, &mu, &window)
			flushCancel()
			log.Println("[DWS] Stopped")
			return
		case <-ticker.C:
			j.flush(ctx, &mu, &window)
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

type enrichFn func(ShortLinkWide, *VisitStats)

func (j *DWSJob) consumeStream(ctx context.Context, reader *kafka.Reader, label string, mu *sync.Mutex, window *map[StatsKey]*VisitStats, enrich enrichFn) {
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
			log.Printf("[DWS-%s] parse error: %v", label, err)
			reader.CommitMessages(ctx, msg)
			continue
		}

		key := buildStatsKey(wide)
		mu.Lock()
		stats, ok := (*window)[key]
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
			(*window)[key] = stats
		}
		enrich(wide, stats)
		mu.Unlock()

		reader.CommitMessages(ctx, msg)
	}
}

func (j *DWSJob) flush(ctx context.Context, mu *sync.Mutex, window *map[StatsKey]*VisitStats) {
	mu.Lock()
	if len(*window) == 0 {
		mu.Unlock()
		return
	}

	// Swap window: old batch for writing, new empty map for next window
	batch := *window
	*window = make(map[StatsKey]*VisitStats)
	mu.Unlock()

	// Set window start/end times (processing time)
	now := time.Now()
	windowStart := now.Add(-windowDuration)
	startTime := FormatDateTime(windowStart)
	endTime := FormatDateTime(now)

	records := make([]*VisitStats, 0, len(batch))
	for _, stats := range batch {
		stats.StartTime = startTime
		stats.EndTime = endTime
		records = append(records, stats)
	}

	if err := j.batchInsert(ctx, records); err != nil {
		log.Printf("[DWS] ClickHouse insert error (%d records): %v", len(records), err)
		// Re-merge failed batch back into window for next flush retry.
		// Previously these records were permanently lost on insert failure.
		mu.Lock()
		for key, stats := range batch {
			if existing, ok := (*window)[key]; ok {
				existing.PV += stats.PV
				existing.UV += stats.UV
			} else {
				(*window)[key] = stats
			}
		}
		mu.Unlock()
	} else {
		log.Printf("[DWS] Flushed %d records to ClickHouse", len(records))
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
		Code:        w.Code,
		Referer:     w.Referer,
		IsNew:       w.IsNew,
		Province:    w.Province,
		City:        w.City,
		IP:          w.IP,
		BrowserName: w.BrowserName,
		OS:          w.OS,
		DeviceType:  w.DeviceType,
	}
}
