package vo

// DomainVO is the view object for domain responses.
type DomainVO struct {
	ID         int64  `json:"id"`
	AccountNo  int64  `json:"account_no"`
	DomainType string `json:"domainType"`
	Value      string `json:"value"`
	State      string `json:"state"`
	CreatedAt  string `json:"createdAt,omitempty"`
}
