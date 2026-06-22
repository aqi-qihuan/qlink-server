package request

// ShortLinkStatusRequest is the request body for toggling short link status.
type ShortLinkStatusRequest struct {
	Code    string `json:"code" binding:"required"`
	GroupID int64  `json:"groupId" binding:"required"`
	State   string `json:"state" binding:"required"` // ACTIVE / INACTIVE / LOCK
}
