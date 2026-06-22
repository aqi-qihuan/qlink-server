package vo

// ApiTokenVO is the response for API token list/create.
type ApiTokenVO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token,omitempty"` // only returned on creation
	Scopes    string `json:"scopes"`
	ExpiredAt string `json:"expiredAt,omitempty"`
	LastUsed  string `json:"lastUsed,omitempty"`
	CreatedAt string `json:"createdAt"`
}
