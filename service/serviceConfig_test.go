package service

import (
	"errors"
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
