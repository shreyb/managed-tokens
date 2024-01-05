// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package notifications contains functions needed to send notifications to the relevant stakeholders for the FIFE Managed Tokens Utilities
//
// The two EmailManager funcs here, NewServiceEmailManager, and NewAdminEmailManager, are the primary interfaces by which calling code
// should send notifications that need to eventually be send via email.  Either of these will sort error notifications properly.const
//
// We expect callers to call NewServiceEmailManager if they are running any of the utilities for a service, and want to abstract away the
// notification sorting and sending.
//
// NewAdminEmailManager can be called if the notifications will only be sent to admins.  In this case, the calling code is expected to
// separately run SendAdminNotifications to actually send the accumulated data.
//
// Both of these EmailManagers require a configured *email object to be passed in.  The implication, then, is that using this package
// to collect and send notifications pre-supposes that one of these notifications will be of the email type.
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
func (p *pushError) GetNode() string    { return p.node }
