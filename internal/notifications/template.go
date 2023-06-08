package notifications

import (
	_ "embed"
	"io"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
)

// TODO unit test prepareMessageFromTemplate

// Admin Errors email template
//
//go:embed templates/adminErrors.txt
var adminErrorsTemplate string

// Service Errors email template
//
//go:embed templates/serviceErrors.txt
var serviceErrorsTemplate string

func prepareMessageFromTemplate(templateReader io.Reader, tmplStruct any) (string, error) {
	var b strings.Builder
	_, err := io.Copy(&b, templateReader)
	if err != nil {
		log.Error("Could not copy template from Reader into Builder")
		return "", err
	}

	emailTemplate, err := template.New("email").Parse(b.String())
	if err != nil {
		log.Error("Could not parse template to send email")
		return "", err
	}

	var tmplBuilder strings.Builder

	if err = emailTemplate.Execute(&tmplBuilder, tmplStruct); err != nil {
		log.Errorf("Failed to execute email template: %s", err)
		return "", err
	}
	return tmplBuilder.String(), nil
}
