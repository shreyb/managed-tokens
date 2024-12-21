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
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	gomail "gopkg.in/gomail.v2"

	"github.com/fermitools/managed-tokens/internal/tracing"
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
	ctx, span := tracer.Start(ctx, "email.sendMessage")
	span.SetAttributes(
		attribute.String("from", e.from),
		attribute.StringSlice("to", e.to),
		attribute.String("subject", e.subject),
		attribute.String("smtpHost", e.smtpHost),
		attribute.Int("smtpPort", e.smtpPort),
	)
	defer span.End()

	emailDialer := gomail.Dialer{
		Host: e.smtpHost,
		Port: e.smtpPort,
	}

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
			err = fmt.Errorf("error sending email: %w", err)
			tracing.LogErrorWithTrace(span, err)
			return err
		}
		tracing.LogSuccessWithTrace(span, "Sent email")
		return nil
	case <-ctx.Done():
		err := fmt.Errorf("error sending email: %w", ctx.Err())
		tracing.LogErrorWithTrace(span, err)
		return err
	}
}
