package vo

// OperationLogVO is the response for operation log queries.
type OperationLogVO struct {
	ID         int64  `json:"id"`
	AccountNo  int64  `json:"accountNo"`
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	ResourceID string `json:"resourceId"`
	IP         string `json:"ip"`
	UserAgent  string `json:"userAgent"`
	Details    string `json:"details"`
	CreatedAt  string `json:"createdAt"`
}
