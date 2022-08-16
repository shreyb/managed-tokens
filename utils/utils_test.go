package utils

import "testing"

func TestCheckForExecutables(t *testing.T) {
	exeMap := map[string]string{
		"true":  "",
		"false": "",
		"ls":    "",
		"cd":    "",
	}

	if err := CheckForExecutables(exeMap); err != nil {
		t.Error(err)
	}
}

func TestCheckRunningUserNotRoot(t *testing.T) {
	if err := CheckRunningUserNotRoot(); err != nil {
		t.Error(err)
	}
}
