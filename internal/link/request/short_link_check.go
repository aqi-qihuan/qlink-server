package request

// ShortLinkCheckRequest check short link code existence (POST /api/link/v1/check).
type ShortLinkCheckRequest struct {
	Code string `json:"code" binding:"required"`
}
