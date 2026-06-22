package request

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexInt accepts both JSON numbers and JSON strings during unmarshalling.
type FlexInt int

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		n, err = strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid int string: %s", s)
		}
		*f = FlexInt(n)
		return nil
	}
	return fmt.Errorf("FlexInt: cannot unmarshal %s", string(data))
}

// VisitRecordPageRequest is the body for POST /api/visit_stats/v1/page_record.
type VisitRecordPageRequest struct {
	Code  string  `json:"code"`
	Page  FlexInt `json:"page"`
	Size  FlexInt `json:"size"`
}

// RegionQueryRequest is the body for POST /api/visit_stats/v1/region_day.
type RegionQueryRequest struct {
	Code      string `json:"code"`
	StartTime string `json:"startTime"` // YYYYMMDD
	EndTime   string `json:"endTime"`   // YYYYMMDD
}

// VisitTrendQueryRequest is the body for POST /api/visit_stats/v1/trend.
type VisitTrendQueryRequest struct {
	Code      string `json:"code"`
	Type      string `json:"type"`      // DAY, HOUR, MINUTE
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// FrequentRequest is the body for POST /api/visit_stats/v1/frequent_ip and /frequent_referer.
type FrequentRequest struct {
	Code      string `json:"code"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// DeviceInfoRequest is the body for POST /api/visit_stats/v1/device_info.
type DeviceInfoRequest struct {
	Code      string `json:"code"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Field     string `json:"field"` // os, browser, device
}
