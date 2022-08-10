// Package notifications contains functions needed to send notifications to the relevant stakeholders for the FIFE Managed Tokens Service
package notifications

// Notification is an interface to various types of notifications sent by a caller to
type Notification interface {
	GetMessage() string
	GetService() string
}

type setupError struct {
	message string
	service string
}

func NewSetupError(message, service string) *setupError {
	return &setupError{
		message: message,
		service: service,
	}
}
func (s *setupError) GetMessage() string { return s.message }
func (s *setupError) GetService() string { return s.service }

type pushError struct {
	message string
	service string
	node    string
}

func NewPushError(message, service, node string) *pushError {
	return &pushError{
		message: message,
		service: service,
		node:    node,
	}
}
func (p *pushError) GetMessage() string { return p.message }
func (p *pushError) GetService() string { return p.service }
