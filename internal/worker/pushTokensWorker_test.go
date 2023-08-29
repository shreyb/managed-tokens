package worker

import (
	"errors"
	"os"
	"testing"

	"github.com/shreyb/managed-tokens/internal/service"
)

// TestSetDefaultRoleFileTemplateValueInExtras ensures that SetDefaultRoleFileTemplateValueInExtras corrently
// sets the Config.Extras["DefaultRoleFileTemplate"] value when called
func TestSetDefaultRoleFileTemplateValueInExtras(t *testing.T) {
	s := service.NewService("testservice")
	testTemplateString := "testtemplate"
	c, _ := NewConfig(
		s,
		SetSupportedExtrasKeyValue(DefaultRoleFileDestinationTemplate, testTemplateString),
	)
	if result := c.Extras[DefaultRoleFileDestinationTemplate]; result != testTemplateString {
		t.Errorf("Wrong template string stored.  Expected %s, got %s", testTemplateString, result)
	}
}

// TestGetDefaultRoleFileTemplateValueFromExtras checks that GetDefaultRoleFileTemplateValueFromExtras properly type-checks and retrieves
// the stored value in the Config.Extras["DefaultRoleFileTemplate"] map
func TestGetDefaultRoleFileTemplateValueFromExtras(t *testing.T) {
	s := service.NewService("testservice")

	type testCase struct {
		description   string
		stored        any
		expectedValue string
		expectedCheck bool
	}
	testCases := []testCase{
		{
			description:   "Valid stored template",
			stored:        "testvalue",
			expectedValue: "testvalue",
			expectedCheck: true,
		},
		{
			description:   "Invalid stored template",
			stored:        5,
			expectedValue: "",
			expectedCheck: false,
		},
	}

	for _, test := range testCases {
		t.Run(test.description,
			func(t *testing.T) {
				c, _ := NewConfig(s)
				c.Extras[DefaultRoleFileDestinationTemplate] = test.stored
				result, check := GetDefaultRoleFileDestinationTemplateValueFromExtras(c)
				if check != test.expectedCheck {
					t.Errorf("Type assertion failed.  Expected type assertion check to return %t, got %t", test.expectedCheck, check)
				}
				if result != test.expectedValue {
					t.Errorf("Got wrong value from config.  Expected %s, got %s", test.expectedValue, result)
				}
			},
		)
	}
}

func TestParseDefaultRoleFileTemplateFromConfig(t *testing.T) {
	s := service.NewService("testservice")
	c, _ := NewConfig(s)
	c.DesiredUID = 12345

	type testCase struct {
		description string
		stored      any
		expected    string
		err         error
	}

	goodTestCases := []testCase{
		{
			description: "Good case",
			stored:      "/tmp/thisisagoodcase_{{.DesiredUID}}_{{.Experiment}}",
			expected:    "/tmp/thisisagoodcase_12345_testservice",
			err:         nil,
		},
		{
			description: "Template string doesn't have any vars to fill - should be OK",
			stored:      "thisshouldstillwork",
			expected:    "thisshouldstillwork",
			err:         nil,
		},
		{
			description: "Wrong type - should produce error",
			stored:      42,
			expected:    "",
			err:         errors.New("error"),
		},
		{
			description: "This template should not execute - so we expect an error",
			stored:      "thisshouldfailwithanexecerror{{.Doesntexist}}",
			expected:    "",
			err:         errors.New("error"),
		},
	}

	for _, test := range goodTestCases {
		t.Run(test.description,
			func(t *testing.T) {
				c.Extras[DefaultRoleFileDestinationTemplate] = test.stored
				result, err := parseDefaultRoleFileDestinationTemplateFromConfig(c)
				if err == nil && test.err != nil {
					t.Errorf("Expected error of type %T, got nil instead", test.expected)
				}
				if result != test.expected {
					t.Errorf("Expected template string value %s, got %s instead", test.expected, result)
				}
			},
		)
	}
}

func TestPrepareDefaultRoleFile(t *testing.T) {
	config := Config{
		Service: service.NewService("myexpt_myrole"),
	}

	testFile, _ := prepareDefaultRoleFile(&config)
	defer os.Remove(testFile)

	data, _ := os.ReadFile(testFile)
	if string(data) != "myrole\n" {
		t.Errorf("Got wrong data in role file.  Expected \"myrole\n\", got \"%s\"", string(data))
	}
}
