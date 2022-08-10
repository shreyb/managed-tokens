package notifications

import (
	"context"
	"io/ioutil"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
	gomail "gopkg.in/gomail.v2"
)

var emailDialer gomail.Dialer // gomail dialer to use to send emails

// Email is an email message configuration
type email struct {
	service        string
	from           string
	to             []string
	subject        string
	smtpHost       string
	smtpPort       int
	templatePath   string // optional template path to use to render template with message passed in
	templateStruct any    // Struct to use with templatePath
}

func (e *email) Service() string { return e.service }
func (e *email) From() string    { return e.from }
func (e *email) To() []string    { return e.to }
func (e *email) Subject() string { return e.subject }

// type Config interface {
// 	Service() string
// 	From() string
// 	To() []string
// 	Subject() string
// }

// WithSlackMessage is an exported func that allows callers to instantiate a slack message in the notifications Manager
func NewEmail(service, from string, to []string, subject, smtpHost string, smtpPort int, templatePath string) *email {
	return &email{
		service:      service,
		from:         from,
		to:           to,
		subject:      subject,
		smtpHost:     smtpHost,
		smtpPort:     smtpPort,
		templatePath: templatePath,
	}
}

// SendMessage sends message as an email based on the Config
func (e *email) SendMessage(ctx context.Context, message string) error {

	emailDialer = gomail.Dialer{
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
				"service":   e.service,
				"recipient": strings.Join(e.to, ", "),
				"email":     e,
			}).Errorf("Error sending email: %s", err)
		} else {
			log.WithFields(log.Fields{
				"service":   e.service,
				"recipient": strings.Join(e.to, ", "),
			}).Info("Sent email")
		}
		return err
	case <-ctx.Done():
		err := ctx.Err()
		if err == context.DeadlineExceeded {
			log.WithFields(log.Fields{
				"service":   e.service,
				"recipient": strings.Join(e.to, ", "),
			}).Error("Error sending email: timeout")
		} else {
			log.WithFields(log.Fields{
				"service":   e.service,
				"recipient": strings.Join(e.to, ", "),
			}).Errorf("Error sending email: %s", err)
		}
		return err
	}

}

func (e *email) prepareEmailWithTemplate() (string, error) {
	var b strings.Builder

	templateData, err := ioutil.ReadFile(e.templatePath)
	if err != nil {
		log.WithFields(log.Fields{
			"service": e.service,
		}).Errorf("Could not read service error template file: %s", err)
		return "", err
	}

	emailTemplate := template.Must(template.New("email").Parse(string(templateData)))
	if err = emailTemplate.Execute(&b, e.templateStruct); err != nil {
		log.WithFields(log.Fields{
			"service": e.service,
		}).Errorf("Failed to execute service email template: %s", err)
		return "", err
	}
	return b.String(), err
}
