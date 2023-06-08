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
			log.WithFields(log.Fields{
				"recipient": strings.Join(e.to, ", "),
				"email":     e,
			}).Errorf("Error sending email: %s", err)
		} else {
			log.WithFields(log.Fields{
				"recipient": strings.Join(e.to, ", "),
			}).Debug("Sent email")
		}
		return err
	case <-ctx.Done():
		err := ctx.Err()
		if err == context.DeadlineExceeded {
			log.WithFields(log.Fields{
				"recipient": strings.Join(e.to, ", "),
			}).Error("Error sending email: timeout")
		} else {
			log.WithFields(log.Fields{
				"recipient": strings.Join(e.to, ", "),
			}).Errorf("Error sending email: %s", err)
		}
		return err
	}

}
