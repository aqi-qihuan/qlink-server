package request

// ShortLinkAddRequest is the request body for creating a short link.
type ShortLinkAddRequest struct {
	GroupID      int64       `json:"groupId"`
	Title        string      `json:"title"`
	OriginalUrl  string      `json:"originalUrl"`
	DomainID     int64       `json:"domainId"`
	DomainType   string      `json:"domainType"`
	Password     string      `json:"password"`
	PasswordHint string      `json:"passwordHint"`
	Expired      interface{} `json:"expired"`
	// Code is set by the controller (which uses a prefixed URL to generate it)
	// and reused by the service consumer. Previously the service re-generated
	// the code from OriginalUrl (without prefix), producing a different code
	// than what was returned to the user — causing the short link to not resolve.
	Code string `json:"code"`
}
