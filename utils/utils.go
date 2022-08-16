package utils

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"strconv"

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
