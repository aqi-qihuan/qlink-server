package request

// ApiTokenCreateRequest is the body for POST /api/account/v1/api_token/create.
type ApiTokenCreateRequest struct {
	Name      string `json:"name" binding:"required"`
	Scopes    string `json:"scopes"`    // e.g. "link:read,link:write"
	ExpiredAt string `json:"expiredAt"` // optional, format 2006-01-02
}

// ApiTokenDeleteRequest is the body for POST /api/account/v1/api_token/delete.
type ApiTokenDeleteRequest struct {
	ID int64 `json:"id" binding:"required"`
}
