package service

import (
	"errors"
	"reflect"
	"testing"
)

type badFunctionalOptError struct {
	msg string
}

func (b badFunctionalOptError) Error() string {
	return b.msg
}

func functionalOptGood(*Config) error {
	return nil
}

func functionalOptBad(*Config) error {
	return badFunctionalOptError{msg: "Bad functional opt"}
}

func TestNewConfig(t *testing.T) {
	type testCase struct {
		description    string
		functionalOpts []func(*Config) error
		expectedError  error
	}
	testCases := []testCase{
		{
			description: "New service.Config with only good functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptGood,
				functionalOptGood,
			},
			expectedError: nil,
		},
		{
			description: "New service.Config with only bad functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptBad,
				functionalOptBad,
			},
			expectedError: badFunctionalOptError{msg: "Bad functional opt"},
		},
		{
			description: "New service.Config with a mix of good and bad functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptGood,
				functionalOptBad,
			},
			expectedError: badFunctionalOptError{msg: "Bad functional opt"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			s := NewService("myawesomeservice")
			_, err := NewConfig(s, tc.functionalOpts...)

			// Equality check of errors
			if !errors.Is(err, tc.expectedError) {
				t.Errorf("Errors do not match.  Expected %s, got %s", tc.expectedError.Error(), err.Error())
			}
		})
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
