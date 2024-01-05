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

package cmdUtils_test

import (
	"fmt"
	"testing"

	"github.com/spf13/viper"

	"github.com/fermitools/managed-tokens/internal/cmdUtils"
	"github.com/fermitools/managed-tokens/internal/service"
)

// TestGetServiceName checks that GetServiceName properly returns the name of the
// service based on the underlying type
func TestGetServiceName(t *testing.T) {
	type testCase struct {
		description  string
		s            service.Service
		expectedName string
	}

	standardService := service.NewService("experiment_role")
	overriddenService := &cmdUtils.ExperimentOverriddenService{standardService, "overrideKey", "overrideKey_role"}

	testCases := []testCase{
		{
			"Standard Service",
			standardService,
			"experiment_role",
		},
		{
			"Experiment-overridden service",
			overriddenService,
			"overrideKey_role",
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if result := cmdUtils.GetServiceName(test.s); result != test.expectedName {
					t.Errorf("Got unexpected service name.  Expected %s, got %s", test.expectedName, result)
				}
			},
		)
	}
}

func TestNewExperimentOverriddenService(t *testing.T) {
	type expectedResult struct {
		experiment, role, name, configName string
	}
	type testCase struct {
		description, serviceName, configKey string
		expectedResult
	}

	testCases := []testCase{
		{
			"Base case - normal override",
			"experiment_role",
			"overrideKey",
			expectedResult{
				"overrideKey",
				"role",
				"experiment_role",
				"overrideKey_role",
			},
		},
		{
			"Override = experiment - should equal a regular service result",
			"experiment_role",
			"experiment",
			expectedResult{
				"experiment",
				"role",
				"experiment_role",
				"experiment_role",
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				s := cmdUtils.NewExperimentOverriddenService(test.serviceName, test.configKey)
				if test.expectedResult.experiment != s.Experiment() {
					t.Errorf("Got wrong result.  Expected %s, got %s", test.expectedResult.experiment, s.Experiment())
				}
				if test.expectedResult.role != s.Role() {
					t.Errorf("Got wrong result.  Expected %s, got %s", test.expectedResult.role, s.Role())
				}
				if test.expectedResult.name != s.Name() {
					t.Errorf("Got wrong result.  Expected %s, got %s", test.expectedResult.name, s.Name())
				}
				if test.expectedResult.configName != s.ConfigName() {
					t.Errorf("Got wrong result.  Expected %s, got %s", test.expectedResult.configName, s.ConfigName())
				}

			},
		)
	}
}

// TestCheckExperimentOverride checks to make sure that a viper configuration experiment
// entry is properly parsed to check for an experimentOverride key
func TestCheckExperimentOverride(t *testing.T) {
	testExperiment := "testExperiment"
	testOverride := "testOverrideExperiment"

	setupViperWithOverride := func() {
		viper.Reset()
		viper.Set(fmt.Sprintf("experiments.%s.experimentOverride", testOverride), testExperiment)
		viper.Set(fmt.Sprintf("experiments.%s", testExperiment), struct{}{})
	}
	setupViperWithoutOverride := func() {
		viper.Reset()
		viper.Set(fmt.Sprintf("experiments.%s", testExperiment), struct{}{})
	}

	type testCase struct {
		description     string
		setupConfigFunc func()
		experiment      string
		expectedResult  string
	}

	testCases := []testCase{
		{
			"No override - should get value of experiment back",
			setupViperWithoutOverride,
			testExperiment,
			testExperiment,
		},
		{
			"Overridden experiment - should get experiment back",
			setupViperWithOverride,
			testOverride,
			testExperiment,
		},
		{
			"Config has overridden experiment, but we're looking at regular experiment - should get experiment back",
			setupViperWithOverride,
			testExperiment,
			testExperiment,
		},
		{
			"Config has overridden experiment, but entry is blank - should get override experiment name back",
			func() {
				viper.Reset()
				viper.Set(fmt.Sprintf("experiments.%s.experimentOverride", testOverride), "")
				viper.Set(fmt.Sprintf("experiments.%s", testExperiment), struct{}{})
			},
			testOverride,
			testOverride,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				defer viper.Reset()
				test.setupConfigFunc()
				if result := cmdUtils.CheckExperimentOverride(test.experiment); result != test.expectedResult {
					t.Errorf("Got wrong return value for experiment.  Expected %s, got %s", test.expectedResult, result)
				}
			},
		)
	}
}
