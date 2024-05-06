// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"testing"

	"github.com/spf13/viper"
)

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
