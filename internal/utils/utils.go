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

// Package utils provides general purpose utilities for the various other packages.
// DEVELOPER NOTE:  This package should ideally NOT depend on any other internal package
package utils

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/cornfeedhobo/pflag"
	"github.com/google/shlex"
	log "github.com/sirupsen/logrus"
)

// CheckForExecutables takes a map of executables of the form {"name_of_executable": "whatever"} and
// checks if each executable is in $PATH.  If the location of the executable in $PATH is already present
// in the map, and can be found on the filesystem it will move to the next executable.  If not, it will
// save the location in the map.  If an executable cannot be found, CheckForExecutables returns an error.
func CheckForExecutables(exeMap map[string]string) error {
	for exe, location := range exeMap {
		// If the location is already saved, continue to the next executable.
		if _, err := os.Stat(location); location != "" && err == nil {
			continue
		}
		pth, err := exec.LookPath(exe)
		if err != nil {
			err := fmt.Errorf("%s was not found in $PATH: %w", exe, err)
			log.Error(err)
			return err
		}
		exeMap[exe] = pth
		log.Debugf("Using %s executable at %s", exe, pth)
	}
	return nil
}

// CheckRunningUserNotRoot checks that the current user running any of the executables is not the system root user
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
	log.Debugf("Current user is %s", currentUser.Username)
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

// IsSliceSubSlice verifies every element within sliceOne is contained within sliceTwo.  Ordering does not matter.
func IsSliceSubSlice[C comparable](sliceOne []C, sliceTwo []C) bool {
	for _, oneElt := range sliceOne {
		if !slices.Contains[[]C, C](sliceTwo, oneElt) {
			return false
		}
	}
	return true
}

// TemplateToCommand takes a *template.Template and a struct, cmdArgs, and executes the template with those args.
// Keep in mind that this means that TemplateToCommand expects that the struct cmdArgs's fields should be exported
// and match up to the fields the template expects.
// TemplateToCommand returns the finalized template string split into a []string.
func TemplateToCommand(templ *template.Template, cmdArgs any) ([]string, error) {
	args := make([]string, 0)

	log.WithFields(log.Fields{
		"cmdArgs":  cmdArgs,
		"template": templ.Name(),
	}).Debug("Executing template with provided args")

	var b strings.Builder
	if err := templ.Execute(&b, cmdArgs); err != nil {
		errMsg := fmt.Sprintf("Could not execute template: %s", err)
		log.Error(errMsg)
		return args, &TemplateExecuteError{errMsg}
	}

	templateString := b.String()
	log.WithField("templateString", templateString).Debug("Filled template string")

	args, err := GetArgsFromTemplate(templateString)
	if err != nil {
		errMsg := fmt.Sprintf("Could not get command arguments from template: %s", err)
		log.Error(errMsg)
		return args, &TemplateArgsError{errMsg}
	}
	return args, nil
}

type TemplateExecuteError struct{ msg string }

func (t *TemplateExecuteError) Error() string { return t.msg }

type TemplateArgsError struct{ msg string }

func (t *TemplateArgsError) Error() string { return t.msg }

// MergeCmdArgs is meant to take a FlagSet that defines defaults and flags for a particular command,
// for example ping, and merges the extraArgs with the defaults.  So for example:
//
//	fs := pflag.NewFlagSet("ping flags", pflag.ContinueOnError)
//
//	fs.StringP("pingFlagW", "W", "5", "")
//	fs.StringP("pingFlagc", "c", "1", "")
//
//	mergedArgs := MergeCmdArgs(fs, []string{}) // returns ([]string{"-W, "5", "-c", "1"}, nil)
//	mergedArgs := MergeCmdArgs(fs, []string{"-4"}) // returns ([]string{"-W, "5", "-c", "1", "-4"}, nil)
//	mergedArgs := MergeCmdArgs(fs, []string{"-W", "6"}) // returns ([]string{"-W, "6", "-c", "1"}, nil)
//	mergedArgs := MergeCmdArgs(fs, []string{"-W", "6", "-4"}) // returns ([]string{"-W, "6", "-c", "1", "-4"}, nil)
//
// Notice in this example that dummy longhand names are given for the flags, because the pflag library
// does not yet support using shorthand flags without longhand flags.
func MergeCmdArgs(fs *pflag.FlagSet, extraArgs []string) ([]string, error) {
	// The passed-in
	fs.ParseErrorsWhitelist = pflag.ParseErrorsWhitelist{UnknownFlags: true}

	// See if we override any of our args by parsing the extraArgs
	fs.Parse(extraArgs)

	flags := make([]string, 0)
	fs.VisitAll(func(f *pflag.Flag) {
		var flagName string
		if f.Shorthand == "" {
			flagName = "--" + f.Name
		} else {
			flagName = "-" + f.Shorthand
		}

		flags = append(flags, flagName)
		flags = append(flags, f.Value.String())
	})
	fmt.Println(fs.GetUnknownFlags())
	flags = append(flags, fs.GetUnknownFlags()...)

	return flags, nil
}
