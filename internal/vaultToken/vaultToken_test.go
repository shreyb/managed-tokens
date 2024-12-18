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
	"path/filepath"
	"testing"

	"github.com/fermitools/managed-tokens/internal/testUtils"
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

type _testUidGetter struct{}

func (u _testUidGetter) getUID() string { return "thisisauid" }

type _testServiceGetter struct{}

func (s _testServiceGetter) getService() string { return "thisisaservice" }

// TestGetAllVaultTokenLocations checks that getAllVaultTokenLocations returns the correct locations for vault tokens
func TestGetAllVaultTokenLocations(t *testing.T) {
	u := _testUidGetter{}
	s := _testServiceGetter{}
	type testCase struct {
		description string
		uidGetter
		serviceGetter
		fileCreators   []func()
		expectedResult []string
		expectedError  error
	}

	testCases := []testCase{
		{
			description:   "Valid locations",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func(){
				func() { createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID()))) },
				func() {
					createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())))
				},
			},
			expectedResult: []string{
				filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID())),
				filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())),
			},
			expectedError: nil,
		},
		{
			description:    "locations that don't exist, since we didn't make them",
			uidGetter:      u,
			serviceGetter:  s,
			fileCreators:   []func(){},
			expectedResult: []string{},
			expectedError:  os.ErrNotExist,
		},
		{
			description:   "default location exists, not condor location",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func(){
				func() { createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID()))) },
			},
			expectedResult: []string{
				filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID())),
			},
			expectedError: os.ErrNotExist,
		},
		{
			description:   "default location does not exist, condor location does",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func(){
				func() {
					createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())))
				},
			},
			expectedResult: []string{
				filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())),
			},
			expectedError: os.ErrNotExist,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				for _, creator := range test.fileCreators {
					creator()
				}

				t.Cleanup(func() {
					for _, location := range test.expectedResult {
						os.Remove(location)
					}
				})

				result, err := getAllVaultTokenLocations(u, s)
				if err != nil && test.expectedError == nil {
					t.Errorf("Expected nil error.  Got %s instead", err)
				}
				if err == nil && test.expectedError != nil {
					t.Errorf("Expected non-nil error.  Got nil")
				}
				if len(result) != len(test.expectedResult) {
					t.Errorf("Expected result length %d.  Got %d instead", len(test.expectedResult), len(result))
				}

				if !testUtils.SlicesHaveSameElements[string](result, test.expectedResult) {
					t.Errorf("Expected locations %s.  Got %s instead", test.expectedResult, result)
				}
			},
		)
	}
}

// TestRemoveServiceVaultTokens checks that removeServiceVaultTokens correctly removes vault tokens
func TestRemoveServiceVaultTokens(t *testing.T) {
	u := _testUidGetter{}
	s := _testServiceGetter{}
	type testCase struct {
		description string
		uidGetter
		serviceGetter
		fileCreators  []func() string
		expectedError error
	}

	testCases := []testCase{
		{
			description:   "Remove valid locations",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func() string{
				func() string { return createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID()))) },
				func() string {
					return createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())))
				},
			},
			expectedError: nil,
		},
		{
			description:   "Remove locations that don't exist",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators:  []func() string{},
			expectedError: os.ErrNotExist,
		},
		{
			description:   "Remove default location exists, not condor location",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func() string{
				func() string { return createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID()))) },
			},
			expectedError: os.ErrNotExist,
		},
		{
			description:   "Remove default location does not exist, condor location does",
			uidGetter:     u,
			serviceGetter: s,
			fileCreators: []func() string{
				func() string {
					return createFileIfNotExist(filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService())))
				},
			},
			expectedError: os.ErrNotExist,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				cleanupFiles := make([]string, 0)
				for _, creator := range test.fileCreators {
					cleanupFiles = append(cleanupFiles, creator())
				}

				t.Cleanup(func() {
					for _, location := range cleanupFiles {
						os.Remove(location)
					}
				})

				err := removeServiceVaultTokens(u, s)
				if err != nil && test.expectedError == nil {
					t.Errorf("Expected nil error.  Got %s instead", err)
				}
				if err == nil && test.expectedError != nil {
					t.Errorf("Expected non-nil error.  Got nil")
				}
				if err != nil && test.expectedError != nil {
					if !errors.Is(err, test.expectedError) {
						t.Errorf("Expected error %s.  Got %s instead", test.expectedError, err)
					}
				}
			},
		)
	}
}

func TestGetCondorVaultTokenLocation(t *testing.T) {
	u := _testUidGetter{}
	s := _testServiceGetter{}
	expectedResult := fmt.Sprintf("/tmp/vt_u%s-%s", u.getUID(), s.getService())
	if result := getCondorVaultTokenLocation(u, s); result != expectedResult {
		t.Errorf("Got wrong result for condor vault token location.  Expected %s, got %s", expectedResult, result)
	}
}

func TestGetDefaultVaultTokenLocation(t *testing.T) {
	u := _testUidGetter{}
	expectedResult := fmt.Sprintf("/tmp/vt_u%s", u.getUID())
	if result := getDefaultVaultTokenLocation(u); result != expectedResult {
		t.Errorf("Got wrong result for condor vault token location.  Expected %s, got %s", expectedResult, result)
	}
}

func createFileIfNotExist(path string) string {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		os.Create(path)
	}
	return path
}
