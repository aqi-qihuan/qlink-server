package request

// AbuseReportRequest creates a new abuse report.
type AbuseReportRequest struct {
	Code        string `json:"code" binding:"required"`
	Reason      string `json:"reason" binding:"required"`
	Description string `json:"description"`
}

// AbuseReportPageRequest lists abuse reports.
type AbuseReportPageRequest struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Size   int    `json:"size"`
}

// AbuseReportResolveRequest resolves/dismisses a report.
type AbuseReportResolveRequest struct {
	ID     int64  `json:"id" binding:"required"`
	Note   string `json:"note"`
	Status string `json:"status" binding:"required"` // RESOLVED / DISMISSED
}

// BrandConfigSaveRequest upserts brand configuration.
type BrandConfigSaveRequest struct {
	SiteName     string `json:"siteName"`
	LogoURL      string `json:"logoUrl"`
	FaviconURL   string `json:"faviconUrl"`
	PrimaryColor string `json:"primaryColor"`
	Copyright    string `json:"copyright"`
}

// URLSafeCheckRequest checks URL safety.
type URLSafeCheckRequest struct {
	URL string `json:"url" binding:"required"`
}
