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

package kerberos

import (
	"slices"
	"testing"
)

func TestParseAndExecuteKinitTemplate(t *testing.T) {
	userPrincipal := "userPrincipal@DOMAIN"
	keytabPath := "/path/to/keytab"
	expected := []string{
		"-k",
		"-t",
		keytabPath,
		userPrincipal,
	}

	if result, err := parseAndExecuteKinitTemplate(keytabPath, userPrincipal); !slices.Equal(result, expected) {
		t.Errorf("Got wrong result.  Expected %v, got %v", expected, result)
	} else if err != nil {
		t.Errorf("Should have gotten nil error.  Got %v instead", err)
	}
}

func TestGetKerberosPrincipalFromKerbListOutput(t *testing.T) {

	type testCase struct {
		description    string
		outputToParse  []byte
		expectedResult string
		expectedErrNil bool
	}

	testCases := []testCase{
		{
			"valid case",
			[]byte("BlahBlah\nDefault principal: thisistheprincipal\nblahblahblah"),
			"thisistheprincipal",
			true,
		},
		{
			"invalid case",
			[]byte("BlahBlahblahblahblah"),
			"",
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result, err := getKerberosPrincipalFromKerbListOutput(test.outputToParse)
				if result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %s, got %s", test.expectedResult, result)
				}
				if test.expectedErrNil && err != nil {
					t.Errorf("Expected error to be nil.  Got %v", err)
				}
			},
		)
	}
}
