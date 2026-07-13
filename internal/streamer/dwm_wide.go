package streamer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	useragent "github.com/mssola/user_agent"
	"github.com/segmentio/kafka-go"
)

const (
	dwmWideSourceTopic = "dwd_link_visit_topic"
	dwmWideSinkTopic   = "dwm_link_visit_topic"
	dwmWideGroupID     = "dwm_short_link_group"
	amapAPIURL         = "https://restapi.amap.com/v3/ip?ip=%s&output=json&key=%s"
)

// DWMWideJob reads from DWD, enriches with device + geo info, writes to DWM-Wide.
type DWMWideJob struct {
	reader     *kafka.Reader
	writer     *kafka.Writer
	httpClient *http.Client
	amapKey    string
	cancel     context.CancelFunc
}

func NewDWMWideJob(brokers string, amapKey string) *DWMWideJob {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(brokers, ","),
		Topic:       dwmWideSourceTopic,
		GroupID:     dwmWideGroupID,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})
	writer := &kafka.Writer{
		Addr:         kafka.TCP(strings.Split(brokers, ",")...),
		Topic:        dwmWideSinkTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 100 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        500,
			MaxIdleConnsPerHost: 300,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	return &DWMWideJob{
		reader:     reader,
		writer:     writer,
		httpClient: client,
		amapKey:    amapKey,
	}
}

func (j *DWMWideJob) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	j.cancel = cancel
	log.Println("[DWM-Wide] Started: consuming from", dwmWideSourceTopic)

	for {
		msg, err := j.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("[DWM-Wide] Stopped")
				return
			}
			log.Printf("[DWM-Wide] fetch error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		result, err := j.process(ctx, msg.Value)
		if err != nil {
			log.Printf("[DWM-Wide] process error: %v", err)
			j.reader.CommitMessages(ctx, msg)
			continue
		}

		err = j.writer.WriteMessages(ctx, kafka.Message{Value: result})
		if err != nil {
			log.Printf("[DWM-Wide] write error: %v", err)
			// Do NOT commit on write failure — let the message be re-delivered.
			time.Sleep(time.Second)
			continue
		}

		j.reader.CommitMessages(ctx, msg)
	}
}

func (j *DWMWideJob) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
	j.reader.Close()
	j.writer.Close()
	j.httpClient.CloseIdleConnections()
}

func (j *DWMWideJob) process(ctx context.Context, raw []byte) ([]byte, error) {
	wide, err := parseAndEnrichDevice(raw)
	if err != nil {
		return nil, err
	}

	// Async geo lookup (non-blocking with context timeout)
	province, city := j.lookupGeo(ctx, wide.IP)
	wide.Province = province
	wide.City = city

	return json.Marshal(wide)
}

// parseAndEnrichDevice extracts device info from User-Agent.
func parseAndEnrichDevice(raw []byte) (ShortLinkWide, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ShortLinkWide{}, err
	}

	data, _ := obj["data"].(map[string]interface{})
	ua := ""
	if data != nil {
		ua, _ = data["user-agent"].(string)
	}

	browserName, os, osVersion, deviceType, manufacturer := parseUserAgent(ua)

	var accountNo int64
	if data != nil {
		if an, ok := data["accountNo"].(float64); ok {
			accountNo = int64(an)
		}
	}

	var ts int64
	if t, ok := obj["ts"].(float64); ok {
		ts = int64(t)
	}

	var isNew int
	if n, ok := obj["is_new"].(float64); ok {
		isNew = int(n)
	}

	wide := ShortLinkWide{
		VisitTime:          ts,
		AccountNo:          accountNo,
		Code:               strVal(obj, "bizId"),
		Referer:            strVal(obj, "referer"),
		IsNew:              isNew,
		IP:                 strVal(obj, "ip"),
		BrowserName:        browserName,
		OS:                 os,
		OSVersion:          osVersion,
		DeviceType:         deviceType,
		DeviceManufacturer: manufacturer,
		UDID:               strVal(obj, "udid"),
	}

	return wide, nil
}

// parseUserAgent extracts device info compatible with Java's DeviceUtil.getDeviceInfo().
func parseUserAgent(uaStr string) (browserName, os, osVersion, deviceType, manufacturer string) {
	if uaStr == "" {
		return "Unknown", "Unknown", "", "COMPUTER", "Unknown"
	}

	ua := useragent.New(uaStr)

	browserName, _ = ua.Browser()
	if browserName == "" {
		browserName = "Unknown"
	}

	os = ua.OS()
	if os == "" {
		os = "Unknown"
	}

	platform := ua.Platform()
	deviceType = classifyDeviceType(platform, os)
	manufacturer = classifyManufacturer(platform, os)
	osVersion = extractOSVersion(uaStr)

	return
}

func classifyDeviceType(platform, os string) string {
	p := strings.ToLower(platform)
	o := strings.ToLower(os)
	if strings.Contains(p, "iphone") || strings.Contains(p, "ipad") ||
		strings.Contains(p, "android") || strings.Contains(o, "android") ||
		strings.Contains(o, "ios") || strings.Contains(p, "mobile") {
		return "MOBILE"
	}
	return "COMPUTER"
}

func classifyManufacturer(platform, os string) string {
	p := strings.ToLower(platform)
	o := strings.ToLower(os)
	if strings.Contains(p, "iphone") || strings.Contains(p, "ipad") ||
		strings.Contains(o, "ios") || strings.Contains(o, "mac") {
		return "APPLE"
	}
	if strings.Contains(o, "android") {
		return "GOOGLE"
	}
	if strings.Contains(o, "windows") {
		return "MICROSOFT"
	}
	return "UNKNOWN"
}

// extractOSVersion mimics Java's DeviceUtil.getOSVersion():
// extract content between ( and ), split by ;, take index [1], trimmed.
func extractOSVersion(ua string) string {
	openIdx := strings.Index(ua, "(")
	closeIdx := strings.Index(ua, ")")
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		return ""
	}
	inner := ua[openIdx+1 : closeIdx]
	parts := strings.Split(inner, ";")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// lookupGeo calls Amap IP geolocation API.
func (j *DWMWideJob) lookupGeo(ctx context.Context, ip string) (province, city string) {
	if j.amapKey == "" || ip == "" {
		return "-", "-"
	}

	apiURL := fmt.Sprintf(amapAPIURL, ip, j.amapKey)

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "-", "-"
	}

	resp, err := j.httpClient.Do(req)
	if err != nil {
		return "-", "-"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "-", "-"
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "-", "-"
	}

	province, _ = result["province"].(string)
	city, _ = result["city"].(string)

	if province == "" || province == "[]" {
		province = "-"
	}
	if city == "" || city == "[]" {
		city = "-"
	}

	return province, city
}

func strVal(obj map[string]interface{}, key string) string {
	v, _ := obj[key].(string)
	return v
}
