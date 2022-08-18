package utils

import (
	"os/exec"
	"testing"
)

type testEnviron struct {
	Krb5ccname string
	key1       string
	key2       string
}

func (t *testEnviron) ToMap() map[string]string {
	m := make(map[string]string)
	m["Krb5ccname"] = "KRB5CCNAME=" + t.Krb5ccname
	m["key1"] = "KEY1=" + t.key1
	m["key2"] = "KEY2=" + t.key2
	return m
}

func (t *testEnviron) ToEnvs() map[string]string {
	m := make(map[string]string)
	m["Krb5ccname"] = "KRB5CCNAME"
	m["key1"] = "KEY1"
	m["key2"] = "KEY2"
	return m
}

type badTestEnviron struct {
	key1 string
	key2 string
}

func (b *badTestEnviron) ToMap() map[string]string {
	m := make(map[string]string)
	m["key1"] = "KEY1=" + b.key1
	m["key2"] = "KEY2=" + b.key2
	return m
}

func (b *badTestEnviron) ToEnvs() map[string]string {
	m := make(map[string]string)
	m["key1"] = "KEY1"
	m["key2"] = "KEY2"
	return m
}

func TestKerberosEnvironmentWrappedCommand(t *testing.T) {
	type testCase struct {
		description               string
		environ                   EnvironmentMapper
		expectedKrb5ccNameSetting string
	}
	testCases := []testCase{
		{
			"Use complete command environment to return kerberos-wrapped command",
			&testEnviron{
				Krb5ccname: "krb5ccnametest",
				key1:       "key1_value",
				key2:       "key2_value",
			},
			"KRB5CCNAME=krb5ccnametest",
		},
		{
			"Use incomplete command environment to return kerberos-wrapped command",
			&badTestEnviron{
				key1: "key1_value",
				key2: "key2_value",
			},
			"",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				cmd := exec.Command("/bin/true")
				cmd = kerberosEnvironmentWrappedCommand(cmd, test.environ)
				found := false
				for _, keyValue := range cmd.Env {
					if keyValue == test.expectedKrb5ccNameSetting {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"Could not find key-value pair %s in command environment",
						test.expectedKrb5ccNameSetting,
					)
				}
			},
		)
	}
}

// TestEnvironmentWrappedCommand makes sure that the service.EnvironmentMapper we pass to
// EnvironmentWrappedCommand gives us the right command environment
func TestEnvironmentWrappedCommand(t *testing.T) {
	environ := &testEnviron{
		Krb5ccname: "krb5ccnametest",
		key1:       "key1_value",
		key2:       "key2_value",
	}
	foundMap := make(map[string]bool)
	for _, envSetting := range environ.ToMap() {
		foundMap[envSetting] = false
	}
	cmd := exec.Command("/bin/true")
	cmd = environmentWrappedCommand(cmd, environ)

	for _, keyValue := range cmd.Env {
		for _, envSetting := range environ.ToMap() {
			if keyValue == envSetting {
				foundMap[envSetting] = true
				break
			}
		}
	}

	for envSetting, found := range foundMap {
		if !found {
			t.Errorf(
				"Could not find key-value pair %s in command environment",
				envSetting,
			)
		}
	}

}
