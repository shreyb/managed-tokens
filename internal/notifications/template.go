package notifications

import (
	_ "embed"
	"errors"
	"io"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
)

// Admin Errors email template
//
//go:embed templates/adminErrors.txt
var adminErrorsTemplate string

// Service Errors email template
//
//go:embed templates/serviceErrors.txt
var serviceErrorsTemplate string

// prepareMessageFromTemplate takes an io.Reader that contains the template data, and populates the template
// with whatever is passed in tmplStruct. tmplStruct should be a struct type that contains the fields contained
// in the template given by templateReader. prepareMessageFromTemplate returns a string containing the
// executed template, and populates the returned error if there was any issue with filling the template.
func prepareMessageFromTemplate(templateReader io.Reader, tmplStruct any) (string, error) {
	var b strings.Builder
	_, err := io.Copy(&b, templateReader)
	if err != nil {
		log.Error("Could not copy template from Reader into Builder")
		return "", errCopyReaderToBuilder
	}

	messageTemplate, err := template.New("email").Parse(b.String())
	if err != nil {
		log.Error("Could not parse template to send message")
		return "", errParseTemplate
	}

	var tmplBuilder strings.Builder

	if err = messageTemplate.Execute(&tmplBuilder, tmplStruct); err != nil {
		log.Errorf("Failed to execute message template: %s", err)
		return "", errExecuteTemplate
	}
	return tmplBuilder.String(), nil
}

var (
	errCopyReaderToBuilder error = errors.New("could not copy template from io.Reader into strings.Builder")
	errParseTemplate       error = errors.New("could not parse template to send message")
	errExecuteTemplate     error = errors.New("could not execute template")
)
