package environment

import (
	"reflect"
	"testing"
)

var cmdEnvFull CommandEnvironment = CommandEnvironment{
	Krb5ccname:          "KRB5CCNAME=krb5ccname_setting",
	CondorCreddHost:     "_condor_CREDD_HOST=condor_credd_host_setting",
	CondorCollectorHost: "_condor_COLLECTOR_HOST=condor_collector_host_setting",
	HtgettokenOpts:      "HTGETTOKENOPTS=htgettokenopts_setting",
}

var cmdEnvPartial CommandEnvironment = CommandEnvironment{
	Krb5ccname:      "KRB5CCNAME=krb5ccname_setting",
	CondorCreddHost: "_condor_CREDD_HOST=condor_credd_host_setting",
}

// TestCommandEnvironmentToMap checks to see if various CommandEnvironments get translated properly to maps
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
				"Krb5ccname":          "KRB5CCNAME=krb5ccname_setting",
				"CondorCreddHost":     "_condor_CREDD_HOST=condor_credd_host_setting",
				"CondorCollectorHost": "_condor_COLLECTOR_HOST=condor_collector_host_setting",
				"HtgettokenOpts":      "HTGETTOKENOPTS=htgettokenopts_setting",
			},
		},
		{
			description:        "Test translating partially-filled CommandEnvironment to a map[string]string",
			CommandEnvironment: cmdEnvPartial,
			expectedResult: map[string]string{
				"Krb5ccname":          "KRB5CCNAME=krb5ccname_setting",
				"CondorCreddHost":     "_condor_CREDD_HOST=condor_credd_host_setting",
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

// TestCommandEnvironmentToEnvs checks to see if various CommandEnvironments get translated properly to maps of environment variable assignments
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
			m := tc.CommandEnvironment.toEnvs()
			if !reflect.DeepEqual(m, tc.expectedResult) {
				t.Errorf("Maps do not match.  Expected %s, got %s", tc.expectedResult, m)
			}
		})
	}
}

// TestCommandEnvironmentToValues checks to see if various CommandEnvironments get translated properly to maps
func TestCommandEnvironmentToValues(t *testing.T) {
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
			m := tc.CommandEnvironment.ToValues()
			if !reflect.DeepEqual(m, tc.expectedResult) {
				t.Errorf("Maps do not match.  Expected %s, got %s", tc.expectedResult, m)
			}
		})
	}
}
