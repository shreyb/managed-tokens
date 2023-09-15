package main

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestGetDevEnvironmentLabel(t *testing.T) {
	// Set env, set config, check default

	// THis is a mockup so far
	reset()
	defer reset()

	configJson := `{"devEnvironmentLabel": "test"}`
	configReader := strings.NewReader(configJson)
	viper.SetConfigType("json")
	viper.ReadConfig(configReader)

	if result := getDevEnvironmentLabel(); result != "test" {
		t.Errorf("Got wrong devEnvironmentLabel.  Expected %s, got %s", "test", result)
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
