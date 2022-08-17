package utils

import (
	"os"
	"testing"
)

func TestIsServiceToken(t *testing.T) {
	type testCase struct {
		description    string
		token          string
		expectedResult bool
	}

	testCases := []testCase{
		{
			"Valid service token",
			"hvs.123456",
			true,
		},
		{
			"Valid legacy service token",
			"s.123456",
			true,
		},
		{
			"Invalid token",
			"thisisnotvalid",
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if result := IsServiceToken(test.token); result != test.expectedResult {
					t.Errorf(
						"Expected result of IsServiceToken on test token %s to be %t.  Got %t instead.",
						test.token,
						test.expectedResult,
						result,
					)
				}
			},
		)
	}
}

func TestValidateVaultToken(t *testing.T) {
	type testCase struct {
		description   string
		rawString     string
		tokenFile     string
		expectedError error
	}

	testCases := []testCase{
		{
			description:   "Valid vault token",
			rawString:     "hvs.123456",
			expectedError: nil,
		},
		{
			description:   "Valid legacy vault token",
			rawString:     "s.123456",
			expectedError: nil,
		},
		{
			description: "Invalid vault token",
			rawString:   "thiswillnotwork",
			expectedError: &InvalidVaultTokenError{
				msg: "vault token failed validation",
			},
		},
	}

	for index, test := range testCases {
		tempFile, _ := os.CreateTemp(os.TempDir(), "testManagedTokens")
		defer os.Remove(tempFile.Name())
		func() {
			defer tempFile.Close()
			_, _ = tempFile.WriteString(test.rawString)
		}()
		testCases[index].tokenFile = tempFile.Name()
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := validateVaultToken(test.tokenFile)
				switch err != nil {
				case true:
					if test.expectedError == nil {
						t.Errorf("Expected nil error.  Got %s instead", err)
						t.Fail()
					} else {
						if _, ok := err.(*InvalidVaultTokenError); !ok {
							t.Errorf("Got wrong type of return error.  Expected *InvalidVaultTokenError")
						}
					}
				case false:
					if test.expectedError != nil {
						t.Errorf("Expected non-nil error.  Got nil")
					}
				}
			},
		)
	}
}
