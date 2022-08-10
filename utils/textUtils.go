package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
	log "github.com/sirupsen/logrus"
)

//TODO:  Check this for extra cruft we don't need

// GetArgsFromTemplate takes a template string and breaks it into a slice of args
func GetArgsFromTemplate(s string) ([]string, error) {
	args, err := shlex.Split(s)
	if err != nil {
		return []string{}, fmt.Errorf("could not split string according to shlex rules: %s", err)
	}

	debugSlice := make([]string, 0)
	for num, f := range args {
		debugSlice = append(debugSlice, strconv.Itoa(num), f)
	}

	log.Debugf("Enumerated args to command are: %s", debugSlice)

	return args, nil
}

// GenerateNewErrorStringForTable is meant to change an error string based on whether it is currently equal to the defaultError.  If the testError and defaultError match, this func will return an error with the text of errStringSlice.  If they don't, then this func will append the contents of errStringSlice onto the testError text, and return a new error with the combined string.  The separator formats how different error strings should be distinguished.   This func should only be used to concatenate error strings
func GenerateNewErrorStringForTable(defaultError, testError error, errStringSlice []string, separator string) error {
	var newErrStringSlice []string
	if testError == defaultError {
		newErrStringSlice = errStringSlice
	} else {
		newErrStringSlice = append([]string{testError.Error()}, errStringSlice...)
	}

	return errors.New(strings.Join(newErrStringSlice, separator))
}
