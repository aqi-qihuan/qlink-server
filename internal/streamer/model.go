package streamer

// RawEvent represents the raw JSON event from ods_link_visit_topic.
// The link service publishes visit events with this structure.
type RawEvent struct {
	IP    string                 `json:"ip"`
	Event string                 `json:"event"`
	BizID string                 `json:"bizId"`
	TS    int64                  `json:"ts"`
	Data  map[string]interface{} `json:"data"`
}

// ShortLinkWide is the enriched wide table record produced by DWM-Wide.
type ShortLinkWide struct {
	Code               string `json:"code"`
	AccountNo          int64  `json:"accountNo"`
	VisitTime          int64  `json:"visitTime"`
	Referer            string `json:"referer"`
	IsNew              int    `json:"isNew"`
	BrowserName        string `json:"browserName"`
	OS                 string `json:"os"`
	OSVersion          string `json:"osVersion"`
	DeviceType         string `json:"deviceType"`
	DeviceManufacturer string `json:"deviceManufacturer"`
	UDID               string `json:"udid"`
	Country            string `json:"country"`
	Province           string `json:"province"`
	City               string `json:"city"`
	ISP                string `json:"isp"`
	IP                 string `json:"ip"`
}

// VisitStats is the aggregated record written to ClickHouse.
type VisitStats struct {
	Code        string
	Referer     string
	IsNew       int
	AccountNo   int64
	Province    string
	City        string
	IP          string
	BrowserName string
	OS          string
	DeviceType  string
	PV          int64
	UV          int64
	StartTime   string
	EndTime     string
	VisitTime   int64
}

// StatsKey is the 9-dimensional composite key for DWS aggregation.
type StatsKey struct {
	Code        string
	Referer     string
	IsNew       int
	Province    string
	City        string
	IP          string
	BrowserName string
	OS          string
	DeviceType  string
}

const insertSQL = `INSERT INTO visit_stats (
	code, referer, isNew, accountNo,
	province, city, ip, browserName, os, deviceType,
	pv, uv, startTime, endTime, visitTime
) VALUES`
