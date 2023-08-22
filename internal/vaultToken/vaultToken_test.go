package vaultToken

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"testing"
)

// TestIsServiceToken checks a number of candidate service tokens and verifies that IsServiceToken correctly identifies whether or not
// a candidate is a service token
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

// TestValidateVaultToken checks that ValidateVaultToken correctly validates vault tokens, or returns the proper error if the token is not valid
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

	tempDir := t.TempDir()
	for index, test := range testCases {
		tempFile, _ := os.CreateTemp(tempDir, "testManagedTokens")
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

func TestValidateServiceVaultToken(t *testing.T) {
	serviceName := "myservice"
	badServiceName := "notmyservice"

	validTokenString := "hvs.123456"
	invalidTokenString := "thiswillnotwork"

	tempDir := t.TempDir()

	type testCase struct {
		description                      string
		serviceName                      string
		writeTokenFileFunc               func() string
		expectedErrorNil                 bool
		expectedErrorIsInvalidVaultToken bool
	}

	testCases := []testCase{
		// Make sure to delete vault token each time.   The fake service name should keep this separate from real stuff:w
		{
			"Valid vault token, service can be found",
			serviceName,
			func() string {
				tokenFileName, _ := getCondorVaultTokenLocation(serviceName)
				b := []byte(validTokenString)
				os.WriteFile(tokenFileName, b, 0644)
				return tokenFileName
			},
			true,
			false,
		},
		{
			"Valid vault token, service can't be found",
			badServiceName,
			func() string {
				tokenFile, _ := os.CreateTemp(tempDir, "managed-tokens-test")
				tokenFileName := tokenFile.Name()
				b := []byte(validTokenString)
				os.WriteFile(tokenFileName, b, 0644)
				return tokenFileName
			},
			false,
			false,
		},
		{
			"invalid vault token, service can't be found",
			badServiceName,
			func() string {
				tokenFile, _ := os.CreateTemp(tempDir, "managed-tokens-test")
				tokenFileName := tokenFile.Name()
				b := []byte(validTokenString)
				os.WriteFile(tokenFileName, b, 0644)
				return tokenFileName
			},
			false,
			false,
		},
		{
			"invalid vault token, service can be found",
			serviceName,
			func() string {
				tokenFileName, _ := getCondorVaultTokenLocation(serviceName)
				b := []byte(invalidTokenString)
				os.WriteFile(tokenFileName, b, 0644)
				return tokenFileName
			},
			false,
			true,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				tokenFile := test.writeTokenFileFunc()
				defer os.Remove(tokenFile)
				err := validateServiceVaultToken(test.serviceName)
				if err != nil && test.expectedErrorNil {
					t.Errorf("Expected nil error.  Got %s instead", err)
				}
				if err == nil && !test.expectedErrorNil {
					t.Error("Got nil error, but expected non-nil error")
				}
				if err != nil && !test.expectedErrorNil && test.expectedErrorIsInvalidVaultToken {
					var e *InvalidVaultTokenError
					if !errors.As(err, &e) {
						t.Errorf("Got wrong kind of error.  Expected InvalidVaultTokenError, got %T", err)
					}
				}
			},
		)
	}
}

func TestGetCondorVaultTokenLocation(t *testing.T) {
	currentUser, _ := user.Current()
	uid := currentUser.Uid
	serviceName := "myService"
	expectedResult := fmt.Sprintf("/tmp/vt_u%s-%s", uid, serviceName)
	result, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		t.Errorf("Expected nil error.  Got %s", err)
	}
	if result != expectedResult {
		t.Errorf("Got wrong result for condor vault token location.  Expected %s, got %s", expectedResult, result)
	}
}

func TestGetDefaultVaultTokenLocation(t *testing.T) {
	currentUser, _ := user.Current()
	uid := currentUser.Uid
	expectedResult := fmt.Sprintf("/tmp/vt_u%s", uid)
	result, err := getDefaultVaultTokenLocation()
	if err != nil {
		t.Errorf("Expected nil error.  Got %s", err)
	}
	if result != expectedResult {
		t.Errorf("Got wrong result for condor vault token location.  Expected %s, got %s", expectedResult, result)
	}

}
