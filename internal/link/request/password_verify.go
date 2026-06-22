package request

// PasswordVerifyRequest is the request body for verifying short link password.
type PasswordVerifyRequest struct {
	Code     string `json:"code" binding:"required"`
	Password string `json:"password" binding:"required"`
}
