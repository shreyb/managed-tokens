package utils

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/shlex"
	log "github.com/sirupsen/logrus"
)

// CheckForExecutables takes a map of executables of the form {"name_of_executable": "whatever"} and
// checks if each executable is in $PATH.  If so, it saves the path in the map.  If not, it returns an error
func CheckForExecutables(exeMap map[string]string) error {
	for exe := range exeMap {
		pth, err := exec.LookPath(exe)
		if err != nil {
			msg := fmt.Sprintf("%s was not found in $PATH", exe)
			log.Error(msg)
			return errors.New(msg)
		}
		exeMap[exe] = pth
		// TODO Make this a debug
		log.Infof("Using %s executable at %s", exe, pth)
	}
	return nil
}

func CheckRunningUserNotRoot() error {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Could not get current user")
		return err
	}
	rootUser, err := user.Lookup("root")
	if err != nil {
		log.Error("Could not lookup root user")
		return err
	}
	if *currentUser == *rootUser {
		msg := "current user is root"
		log.Error(msg)
		return errors.New(msg)
	}
	//TODO Make this a debug
	log.Infof("Current user is %s", currentUser.Username)
	return nil
}

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

func IsSliceSubSlice(sliceOne any, sliceTwo any) error {
	var reflectOne, reflectTwo reflect.Value
	switch reflect.TypeOf(sliceOne).Kind() {
	case reflect.Slice:
		reflectOne = reflect.ValueOf(sliceOne)
	default:
		return errors.New("unsupported type for CompareSlices")
	}
	switch reflect.TypeOf(sliceTwo).Kind() {
	case reflect.Slice:
		reflectTwo = reflect.ValueOf(sliceTwo)
	default:
		return errors.New("unsupported type for CompareSlices")
	}

	for indexOne := 0; indexOne < reflectOne.Len(); indexOne++ {
		found := false
		for indexTwo := 0; indexTwo < reflectTwo.Len(); indexTwo++ {
			if reflectOne.Index(indexOne).Interface() == reflectTwo.Index(indexTwo).Interface() {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("could not find value %v in both slices", reflectOne.Index(indexOne))
		}
	}
	return nil
}

// TODO Unit test this
func TemplateToCommand(templ *template.Template, cmdArgs any) ([]string, error) {
	args := make([]string, 0)

	log.WithFields(log.Fields{
		"cmdArgs":  cmdArgs,
		"template": templ.Name(),
	}).Debug("Executing template with provided args")

	var b strings.Builder
	if err := templ.Execute(&b, cmdArgs); err != nil {
		errMsg := "Could not execute template"
		log.Error(errMsg)
		return args, &TemplateExecuteError{errMsg}
	}

	templateString := b.String()
	log.WithField("templateString", templateString).Debug("Filled template string")

	args, err := GetArgsFromTemplate(templateString)
	if err != nil {
		errMsg := "Could not get command arguments from template"
		log.Error(errMsg)
		return args, &TemplateArgsError{errMsg}
	}
	return args, nil
}

type TemplateExecuteError struct{ msg string }

func (t *TemplateExecuteError) Error() string { return t.msg }

type TemplateArgsError struct{ msg string }

func (t *TemplateArgsError) Error() string { return t.msg }
