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

package notifications

import (
	"context"
	"strings"

	log "github.com/sirupsen/logrus"
	gomail "gopkg.in/gomail.v2"
)

// Email is an email message configuration
type email struct {
	from     string
	to       []string
	subject  string
	smtpHost string
	smtpPort int
}

func (e *email) From() string    { return e.from }
func (e *email) To() []string    { return e.to }
func (e *email) Subject() string { return e.subject }

// NewEmail returns an *email that can be used to send a message using SendMessage()
func NewEmail(from string, to []string, subject, smtpHost string, smtpPort int) *email {
	return &email{
		from:     from,
		to:       to,
		subject:  subject,
		smtpHost: smtpHost,
		smtpPort: smtpPort,
	}
}

// sendMessage sends message as an email based on the email object configuration
func (e *email) sendMessage(ctx context.Context, message string) error {
	emailDialer := gomail.Dialer{
		Host: e.smtpHost,
		Port: e.smtpPort,
	}
	funcLogger := log.WithField("recipient", strings.Join(e.to, ", "))

	m := gomail.NewMessage()
	m.SetHeader("From", e.from)
	m.SetHeader("To", e.to...)
	m.SetHeader("Subject", e.subject)
	m.SetBody("text/plain", message)

	c := make(chan error)
	go func() {
		defer close(c)
		err := emailDialer.DialAndSend(m)
		c <- err
	}()

	select {
	case err := <-c:
		if err != nil {
			funcLogger.WithField("email", e).Errorf("Error sending email: %s", err)
		} else {
			funcLogger.Debug("Sent email")
		}
		return err
	case <-ctx.Done():
		err := ctx.Err()
		if err == context.DeadlineExceeded {
			funcLogger.Error("Error sending email: timeout")
		} else {
			funcLogger.Errorf("Error sending email: %s", err)
		}
		return err
	}
}
