package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestGetDevEnvironmentLabel(t *testing.T) {
	type testCase struct {
		description    string
		envSetting     string
		configSetting  string
		expectedResult string
	}

	testCases := []testCase{
		{
			"Nothing set",
			"",
			"",
			devEnvironmentLabelDefault,
		},
		{
			"Config set",
			"",
			"testConfig",
			"testConfig",
		},
		{
			"Env set",
			"testEnv",
			"",
			"testEnv",
		},
		{
			"Config and Env set",
			"testEnv",
			"testConfig",
			"testEnv",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				reset()
				defer reset()
				if test.envSetting != "" {
					os.Setenv("MANAGED_TOKENS_DEV_ENVIRONMENT_LABEL", test.envSetting)
				}
				if test.configSetting != "" {
					configJson := fmt.Sprintf(`{"devEnvironmentLabel": "%s"}`, test.configSetting)
					configReader := strings.NewReader(configJson)
					viper.SetConfigType("json")
					viper.ReadConfig(configReader)
				}

				if result := getDevEnvironmentLabel(); result != test.expectedResult {
					t.Errorf("Got wrong devEnvironmentLabel.  Expected %s, got %s", "test", result)
				}

			},
		)
	}
}

func TestGetPrometheusJobName(t *testing.T) {
	type testCase struct {
		description           string
		jobNameSetting        string
		devEnvironmentLabel   string
		expectedJobNameResult string
	}

	testCases := []testCase{
		{
			"Completely default",
			"",
			devEnvironmentLabelDefault,
			"managed_tokens",
		},
		{
			"Set different jobname than default",
			"myjobname",
			devEnvironmentLabelDefault,
			"myjobname",
		},
		{
			"Set different devEnvLabel than default",
			"",
			"test",
			"managed_tokens_test",
		},
		{
			"Set different jobname and devEnvLabel than default",
			"myjobname",
			"test",
			"myjobname_test",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				reset()
				defer reset()
				if test.jobNameSetting != "" {
					viper.Set("prometheus.jobname", test.jobNameSetting)
				}
				if test.devEnvironmentLabel != "" {
					devEnvironmentLabel = test.devEnvironmentLabel
				}
				if result := getPrometheusJobName(); result != test.expectedJobNameResult {
					t.Errorf("Got wrong jobname.  Expected %s, got %s", test.expectedJobNameResult, result)
				}
			},
		)
	}
}

func reset() {
	viper.Reset()
	devEnvironmentLabel = ""
}
