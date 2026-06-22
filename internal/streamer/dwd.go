package streamer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"

	"github.com/aqi/qlink-server/internal/common/util"
)

const (
	odsTopic    = "ods_link_visit_topic"
	dwdTopic    = "dwd_link_visit_topic"
	dwdGroupID  = "dwd_short_link_group"
)

// DWDJob reads from ODS, enriches events, writes to DWD.
type DWDJob struct {
	reader *kafka.Reader
	writer *kafka.Writer
	rdb    *redis.Client
	cancel context.CancelFunc
}

func NewDWDJob(brokers string, rdb *redis.Client) *DWDJob {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(brokers, ","),
		Topic:       odsTopic,
		GroupID:     dwdGroupID,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})
	writer := &kafka.Writer{
		Addr:         kafka.TCP(strings.Split(brokers, ",")...),
		Topic:        dwdTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 100 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}
	return &DWDJob{reader: reader, writer: writer, rdb: rdb}
}

func (j *DWDJob) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	j.cancel = cancel
	log.Println("[DWD] Started: consuming from", odsTopic)

	for {
		msg, err := j.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("[DWD] Stopped")
				return
			}
			log.Printf("[DWD] fetch error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		result, err := j.process(ctx, msg.Value)
		if err != nil {
			log.Printf("[DWD] process error: %v", err)
			j.reader.CommitMessages(ctx, msg)
			continue
		}

		err = j.writer.WriteMessages(ctx, kafka.Message{Value: []byte(result)})
		if err != nil {
			log.Printf("[DWD] write error: %v", err)
		}

		j.reader.CommitMessages(ctx, msg)
	}
}

func (j *DWDJob) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
	j.reader.Close()
	j.writer.Close()
}

func (j *DWDJob) process(ctx context.Context, raw []byte) (string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("json parse: %w", err)
	}

	// Generate device unique ID
	udid := generateDeviceID(obj)
	obj["udid"] = udid

	// Extract referer host
	referer := extractReferer(obj)
	obj["referer"] = referer

	// New/old visitor detection via Redis
	isNew := j.checkNewVisitor(ctx, udid, obj)
	obj["is_new"] = isNew

	result, _ := json.Marshal(obj)
	return string(result), nil
}

// generateDeviceID computes MD5 of TreeMap(ip, event, bizId, userAgent).toString()
// Compatible with Java's DeviceUtil.geneWebUniqueDeviceId(TreeMap).
func generateDeviceID(obj map[string]interface{}) string {
	ip, _ := obj["ip"].(string)
	event, _ := obj["event"].(string)
	bizID, _ := obj["bizId"].(string)

	var userAgent string
	if data, ok := obj["data"].(map[string]interface{}); ok {
		userAgent, _ = data["user-agent"].(string)
	}

	// Java TreeMap.toString() format with keys sorted alphabetically:
	// {bizId=xxx, event=xxx, ip=xxx, userAgent=xxx}
	input := fmt.Sprintf("{bizId=%s, event=%s, ip=%s, userAgent=%s}", bizID, event, ip, userAgent)
	return util.MD5(input)
}

// extractReferer extracts the host from the referer URL in the event data.
func extractReferer(obj map[string]interface{}) string {
	data, ok := obj["data"].(map[string]interface{})
	if !ok {
		return ""
	}
	referer, _ := data["referer"].(string)
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return u.Host
}

// checkNewVisitor determines if the visitor is new (1) or returning (0).
// Uses Redis to track the last visit date per device ID.
func (j *DWDJob) checkNewVisitor(ctx context.Context, udid string, obj map[string]interface{}) int {
	if udid == "" {
		return 1
	}

	ts, _ := obj["ts"].(float64)
	currentDate := FormatDate(int64(ts))
	redisKey := "streamer:visitor:" + udid

	stored, err := j.rdb.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		// First time visitor
		j.rdb.Set(ctx, redisKey, currentDate, 0) // no expiry, matches Java (no TTL)
		return 1
	}
	if err != nil {
		log.Printf("[DWD] Redis GET error: %v", err)
		return 1
	}

	if strings.EqualFold(stored, currentDate) {
		return 0 // returning visitor
	}

	// Different day ??new visitor, update state
	j.rdb.Set(ctx, redisKey, currentDate, 0)
	return 1
}
