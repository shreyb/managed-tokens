// Package notifications contains functions needed to send notifications to the relevant stakeholders for the FIFE Managed Tokens Utilities
package notifications

// Notification is an interface to various types of notifications sent by a caller to
type Notification interface {
	GetMessage() string
	GetService() string
}

// setupError is a Notification for an error that occurs during the setup phase of a utility.
type setupError struct {
	message string
	service string
}

// NewSetupError returns a *setupError that can be populated and then sent through an EmailManager
func NewSetupError(message, service string) *setupError {
	return &setupError{
		message: message,
		service: service,
	}
}
func (s *setupError) GetMessage() string { return s.message }
func (s *setupError) GetService() string { return s.service }

// pushError is a Notification for an error that occurs while pushing tokens to service nodes
type pushError struct {
	message string
	service string
	node    string
}

// NewPushError returns a *pushError that can be populated and then sent through an EmailManager
func NewPushError(message, service, node string) *pushError {
	return &pushError{
		message: message,
		service: service,
		node:    node,
	}
}
func (p *pushError) GetMessage() string { return p.message }
func (p *pushError) GetService() string { return p.service }
