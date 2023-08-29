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
