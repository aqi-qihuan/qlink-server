package request

// ShortLinkUpdateRequest is the request body for updating a short link.
type ShortLinkUpdateRequest struct {
	ID           int64  `json:"mappingId"`
	GroupID      int64  `json:"groupId"`
	Title        string `json:"title"`
	OriginalUrl  string `json:"originalUrl"`
	Domain       string `json:"domain"`
	DomainType   string `json:"domainType"`
	DomainId     int64  `json:"domainId"`
	Code         string `json:"code"`
	Password     string `json:"password"`
	PasswordHint string `json:"passwordHint"`
	Expired      string `json:"expired"`
}
