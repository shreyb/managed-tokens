package service

import (
	"testing"
)

func TestNewService(t *testing.T) {
	type testCase struct {
		description        string
		serviceName        string
		expectedExperiment string
		expectedRole       string
		expectedName       string
	}
	testCases := []testCase{
		{
			description:        "Experiment with no role (should assign default role)",
			serviceName:        "myawesomeexperiment",
			expectedExperiment: "myawesomeexperiment",
			expectedRole:       DefaultRole,
			expectedName:       "myawesomeexperiment",
		},
		{
			description:        "Experiment with role (should parse out experiment and role",
			serviceName:        "myreallycoolexperiment_superrole",
			expectedExperiment: "myreallycoolexperiment",
			expectedRole:       "superrole",
			expectedName:       "myreallycoolexperiment_superrole",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			s := NewService(testCase.serviceName)
			if s.Experiment() != testCase.expectedExperiment {
				t.Errorf("New service does not have the expected experiment name.  Wanted %s, got %s", testCase.expectedExperiment, s.Experiment())
			}
			if s.Role() != testCase.expectedRole {
				t.Errorf("New service does not have the expected role name.  Wanted %s, got %s", testCase.expectedRole, s.Role())
			}
			if s.Name() != testCase.expectedName {
				t.Errorf("New service does not have the expected service name.  Wanted %s, got %s", testCase.expectedName, s.Name())
			}
		})
	}
}
