package utils

import (
	"fmt"
	"os/exec"
)

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
