package vo

// DashboardVO is the response for dashboard API.
type DashboardVO struct {
	TotalPV       int64            `json:"totalPv"`
	TotalUV       int64            `json:"totalUv"`
	TotalIPCount  int64            `json:"totalIpCount"`
	TodayPV       int64            `json:"todayPv"`
	TodayUV       int64            `json:"todayUv"`
	TodayNewUV    int64            `json:"todayNewUv"`
	TopLinks      []TopLinkVO      `json:"topLinks"`
	DailyTrend    []DashboardTrendVO `json:"dailyTrend"`
}

// TopLinkVO represents a top-ranked short link.
type TopLinkVO struct {
	Code    string `json:"code"`
	PVCount int64  `json:"pvCount"`
}

// DashboardTrendVO is a single day's aggregated stats for dashboard.
type DashboardTrendVO struct {
	Date    string `json:"date"`
	PVCount int64  `json:"pvCount"`
	UVCount int64  `json:"uvCount"`
}

// AnalysisVO is the aggregated analysis response for a single link.
type AnalysisVO struct {
	Code       string              `json:"code"`
	TotalPV    int64               `json:"totalPv"`
	TotalUV    int64               `json:"totalUv"`
	NewUV      int64               `json:"newUv"`
	TopReferers []RefererItemVO    `json:"topReferers"`
	Regions    []RegionDayVO       `json:"regions"`
	Devices    []DeviceItemVO      `json:"devices"`
	OSList     []DeviceItemVO      `json:"osList"`
	Browsers   []DeviceItemVO      `json:"browsers"`
	DailyTrend []VisitTrendVO      `json:"dailyTrend"`
}

// GeoVO is map-ready geo data.
type GeoVO struct {
	Name  string `json:"name"`  // province name
	Value int64  `json:"value"` // pv count
}

