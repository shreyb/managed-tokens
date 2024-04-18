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

package vaultToken

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"slices"
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

func TestGetAllVaultTokenLocations(t *testing.T) {
	serviceName := "mytestservice"
	user, _ := user.Current()

	goodDefaultFile := func() string { return createFileIfNotExist(fmt.Sprintf("/tmp/vt_u%s", user.Uid)) }
	goodCondorFile := func() string { return createFileIfNotExist(fmt.Sprintf("/tmp/vt_u%s-%s", user.Uid, serviceName)) }
	badFile := func() string { return "thispathdoesnotexist" }

	clearFiles := func() {
		os.Remove(goodDefaultFile())
		os.Remove(goodCondorFile())
		os.Remove(badFile())
	}

	type testCase struct {
		description    string
		fileCreators   []func() string
		expectedResult []string
	}

	testCases := []testCase{
		{
			"Can find both locations",
			[]func() string{goodDefaultFile, goodCondorFile},
			[]string{goodDefaultFile(), goodCondorFile()},
		},
		{
			"Can find default file, not condor",
			[]func() string{goodDefaultFile, badFile},
			[]string{goodDefaultFile()},
		},
		{
			"Can find condor file, not default",
			[]func() string{badFile, goodCondorFile},
			[]string{goodCondorFile()},
		},
		{
			"Can't find either file",
			[]func() string{badFile, badFile},
			[]string{},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				clearFiles()
				for _, f := range test.fileCreators {
					defaultFile := f()
					defer os.Remove(defaultFile)
				}
				result, _ := GetAllVaultTokenLocations(serviceName)
				if !slices.Equal(result, test.expectedResult) {
					t.Errorf("Got wrong result.  Expected %v, got %v", test.expectedResult, result)
				}
			},
		)
	}
}

func createFileIfNotExist(path string) string {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		os.Create(path)
	}
	return path
}
