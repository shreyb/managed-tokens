package utils

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"regexp"

	log "github.com/sirupsen/logrus"
)

var servicePattern = regexp.MustCompile(`([[:alnum:]]+)_([[:alnum:]]+)`)

// CheckForExecutables takes a map of executables of the form {"name_of_executable": "whatever"} and
// checks if each executable is in $PATH.  If so, it saves the path in the map.  If not, it returns an error
func CheckForExecutables(exeMap map[string]string) error {
	for exe := range exeMap {
		pth, err := exec.LookPath(exe)
		if err != nil {
			return fmt.Errorf("%s was not found in $PATH", exe)
		}
		exeMap[exe] = pth
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

func ParseServiceToExperimentAndRole(service string) ([]string, error) {
	matches := servicePattern.FindStringSubmatch(service)
	if len(matches) < 3 {
		msg := "could not parse experiment and role from service"
		log.WithField("service", service).Error(msg)
		return []string{}, errors.New(msg)
	}

	log.WithFields(log.Fields{
		"service":    service,
		"experiment": matches[1],
		"role":       matches[2],
	}).Debug("Parsed experiment and role from service")
	return matches[1:], nil
}
