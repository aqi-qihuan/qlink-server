package request

// OperationLogPageRequest is the body for POST /api/operation_log/v1/page.
type OperationLogPageRequest struct {
	Action     string `json:"action"`
	ResourceID string `json:"resourceId"`
	StartTime  string `json:"startTime"` // 2006-01-02
	EndTime    string `json:"endTime"`
	Page       int    `json:"page"`
	Size       int    `json:"size"`
}

// DomainCreateRequest is the body for POST /api/domain/v1/create.
type DomainCreateRequest struct {
	DomainType string `json:"domainType" binding:"required"`
	Value      string `json:"value" binding:"required"`
}

// DomainUpdateRequest is the body for POST /api/domain/v1/update.
type DomainUpdateRequest struct {
	ID         int64  `json:"id" binding:"required"`
	DomainType string `json:"domainType"`
	Value      string `json:"value"`
}

// DomainDeleteRequest is the body for POST /api/domain/v1/delete.
type DomainDeleteRequest struct {
	ID int64 `json:"id" binding:"required"`
}

// DomainStatusRequest is the body for POST /api/domain/v1/status.
type DomainStatusRequest struct {
	ID    int64  `json:"id" binding:"required"`
	State string `json:"state" binding:"required"` // ACTIVE / INACTIVE
}

// BatchDeleteRequest is the body for POST /api/link/v1/batch_delete.
type BatchDeleteRequest struct {
	GroupID int64   `json:"groupId" binding:"required"`
	IDs     []int64 `json:"ids" binding:"required"`
}

// BatchStatusRequest is the body for POST /api/link/v1/batch_status.
type BatchStatusRequest struct {
	GroupID int64  `json:"groupId" binding:"required"`
	Code    string `json:"code" binding:"required"` // one short link code
	State   string `json:"state" binding:"required"`
}
