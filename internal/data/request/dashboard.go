package request

// DashboardRequest is the body for POST /api/visit_stats/v1/dashboard.
type DashboardRequest struct {
	StartTime string `json:"startTime"` // YYYYMMDD, default 30 days ago
	EndTime   string `json:"endTime"`   // YYYYMMDD, default today
}

// AnalysisRequest is the body for POST /api/visit_stats/v1/analysis.
type AnalysisRequest struct {
	Code      string `json:"code" binding:"required"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// ExportRequest is the body for POST /api/visit_stats/v1/export.
type ExportRequest struct {
	Code      string `json:"code" binding:"required"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// GeoRequest is the body for POST /api/visit_stats/v1/geo.
type GeoRequest struct {
	Code      string `json:"code"` // optional, empty = all user links
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}
