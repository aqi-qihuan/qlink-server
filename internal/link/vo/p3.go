package vo

// AbuseReportVO is the response for abuse reports.
type AbuseReportVO struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Reason      string `json:"reason"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
}

// BrandConfigVO is the response for brand configuration.
type BrandConfigVO struct {
	SiteName     string `json:"siteName"`
	LogoURL      string `json:"logoUrl"`
	FaviconURL   string `json:"faviconUrl"`
	PrimaryColor string `json:"primaryColor"`
	Copyright    string `json:"copyright"`
}

// URLSafeCheckVO is the response for URL safety check.
type URLSafeCheckVO struct {
	Safe      bool   `json:"safe"`
	Message   string `json:"message"`
	SSL       bool   `json:"ssl"`
	Reachable bool   `json:"reachable"`
}
