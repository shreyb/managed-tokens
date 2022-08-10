package notifications

// Config contains the information needed to send notifications from the Managed Tokens service
type Config interface {
	Service() string
	From() string
	To() []string
	SetFrom(string) error
	SetTo([]string) error
}
