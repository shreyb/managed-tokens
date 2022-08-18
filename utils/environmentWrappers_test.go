package utils

import (
	"os/exec"
	"reflect"
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

var cmdEnvFull CommandEnvironment = CommandEnvironment{
	Krb5ccname:          "krb5ccname_setting",
	CondorCreddHost:     "condor_credd_host_setting",
	CondorCollectorHost: "condor_collector_host_setting",
	HtgettokenOpts:      "htgettokenopts_setting",
}

var cmdEnvPartial CommandEnvironment = CommandEnvironment{
	Krb5ccname:      "krb5ccname_setting",
	CondorCreddHost: "condor_credd_host_setting",
}

func TestCommandEnvironmentToMap(t *testing.T) {
	type testCase struct {
		description string
		CommandEnvironment
		expectedResult map[string]string
	}
	testCases := []testCase{
		{
			description:        "Test translating filled CommandEnvironment to a map[string]string",
			CommandEnvironment: cmdEnvFull,
			expectedResult: map[string]string{
				"Krb5ccname":          "krb5ccname_setting",
				"CondorCreddHost":     "condor_credd_host_setting",
				"CondorCollectorHost": "condor_collector_host_setting",
				"HtgettokenOpts":      "htgettokenopts_setting",
			},
		},
		{
			description:        "Test translating partially-filled CommandEnvironment to a map[string]string",
			CommandEnvironment: cmdEnvPartial,
			expectedResult: map[string]string{
				"Krb5ccname":          "krb5ccname_setting",
				"CondorCreddHost":     "condor_credd_host_setting",
				"CondorCollectorHost": "",
				"HtgettokenOpts":      "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			m := tc.CommandEnvironment.ToMap()
			if !reflect.DeepEqual(m, tc.expectedResult) {
				t.Errorf("Maps do not match.  Expected %s, got %s", tc.expectedResult, m)
			}
		})
	}
}

func TestCommandEnvironmentToEnvs(t *testing.T) {
	type testCase struct {
		description string
		CommandEnvironment
		expectedResult map[string]string
	}

	staticExpectedResult := map[string]string{
		"Krb5ccname":          "KRB5CCNAME",
		"CondorCreddHost":     "_condor_CREDD_HOST",
		"CondorCollectorHost": "_condor_COLLECTOR_HOST",
		"HtgettokenOpts":      "HTGETTOKENOPTS",
	}

	testCases := []testCase{
		{
			description:        "Take full CommandEnvironment, and return all translations to environment variables",
			CommandEnvironment: cmdEnvFull,
			expectedResult:     staticExpectedResult,
		},
		{
			description:        "Take partial CommandEnvironment, and make sure we still return all possible translations to environment variables",
			CommandEnvironment: cmdEnvPartial,
			expectedResult:     staticExpectedResult,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			m := tc.CommandEnvironment.ToEnvs()
			if !reflect.DeepEqual(m, tc.expectedResult) {
				t.Errorf("Maps do not match.  Expected %s, got %s", tc.expectedResult, m)
			}
		})
	}
}
