package streamer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

const (
	dwmUVSourceTopic = "dwm_link_visit_topic"
	dwmUVSinkTopic   = "dwm_unique_visitor_topic"
	dwmUVGroupID     = "dwm_unique_visitor_group"
	uvTTL            = 24 * time.Hour
)

// DWMUVJob reads from DWM-Wide, filters to unique visitors only, writes to DWM-UV.
type DWMUVJob struct {
	reader *kafka.Reader
	writer *kafka.Writer
	rdb    *redis.Client
	cancel context.CancelFunc
}

func NewDWMUVJob(brokers string, rdb *redis.Client) *DWMUVJob {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(brokers, ","),
		Topic:       dwmUVSourceTopic,
		GroupID:     dwmUVGroupID,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})
	writer := &kafka.Writer{
		Addr:         kafka.TCP(strings.Split(brokers, ",")...),
		Topic:        dwmUVSinkTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 100 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}
	return &DWMUVJob{reader: reader, writer: writer, rdb: rdb}
}

func (j *DWMUVJob) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	j.cancel = cancel
	log.Println("[DWM-UV] Started: consuming from", dwmUVSourceTopic)

	for {
		msg, err := j.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("[DWM-UV] Stopped")
				return
			}
			log.Printf("[DWM-UV] fetch error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		isUnique, err := j.checkUniqueVisitor(ctx, msg.Value)
		if err != nil {
			log.Printf("[DWM-UV] check error: %v", err)
			// Do NOT commit on check failure (e.g. Redis down) — re-deliver.
			time.Sleep(time.Second)
			continue
		}

		if isUnique {
			err = j.writer.WriteMessages(ctx, kafka.Message{Value: msg.Value})
			if err != nil {
				log.Printf("[DWM-UV] write error: %v", err)
				// Do NOT commit on write failure — re-deliver.
				time.Sleep(time.Second)
				continue
			}
		}

		j.reader.CommitMessages(ctx, msg)
	}
}

func (j *DWMUVJob) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
	j.reader.Close()
	j.writer.Close()
}

// checkUniqueVisitor returns true if this is the first visit for this udid today.
// Uses Redis SETNX with 1-day TTL (matches Java's ValueState + TTL of 1 day).
func (j *DWMUVJob) checkUniqueVisitor(ctx context.Context, raw []byte) (bool, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}

	udid, _ := obj["udid"].(string)
	if udid == "" {
		return true, nil
	}

	visitTime, _ := obj["visitTime"].(float64)
	currentDate := FormatDate(int64(visitTime))
	redisKey := "streamer:uv:" + udid

	// SETNX: set if not exists, with 1-day TTL
	set, err := j.rdb.SetNX(ctx, redisKey, currentDate, uvTTL).Result()
	if err != nil {
		// Redis failure: return error so the message is re-delivered, NOT
		// committed. Previously returned true (unique), causing UV counts
		// to be severely inflated during Redis outages.
		return false, fmt.Errorf("redis SETNX failed: %w", err)
	}

	if set {
		// Key was newly created → first visit today → unique
		return true, nil
	}

	// Key already exists → check if same date
	stored, err := j.rdb.Get(ctx, redisKey).Result()
	if err != nil && err != redis.Nil {
		return false, fmt.Errorf("redis GET failed: %w", err)
	}

	if strings.EqualFold(stored, currentDate) {
		// Same date → already counted → not unique
		return false, nil
	}

	// Different date → update and count as unique
	j.rdb.Set(ctx, redisKey, currentDate, uvTTL)
	return true, nil
}
