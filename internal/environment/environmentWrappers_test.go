package environment

import (
	"context"
	"os/exec"
	"testing"

	"github.com/shreyb/managed-tokens/internal/utils"
)

// TestKerberosEnvironmentWrappedCommand makes sure that KerberosEnvironmentWrappedCommand
// sets the kerberos-related environment variables properly
func TestKerberosEnvironmentWrappedCommand(t *testing.T) {
	type testCase struct {
		description               string
		environ                   *CommandEnvironment
		expectedKrb5ccNameSetting string
	}
	testCases := []testCase{
		{
			"Use minimal command environment to return kerberos-wrapped command",
			&CommandEnvironment{
				Krb5ccname: "KRB5CCNAME=krb5ccnametest",
			},
			"KRB5CCNAME=krb5ccnametest",
		},
		{
			"Use complete command environment to return kerberos-wrapped command",
			&CommandEnvironment{
				Krb5ccname:          "KRB5CCNAME=krb5ccnametest",
				CondorCreddHost:     "_condor_CREDD_HOST=foo",
				CondorCollectorHost: "_condor_COLLECTOR_HOST=bar",
				HtgettokenOpts:      "HTGETTOKENOPTS=baz",
			},
			"KRB5CCNAME=krb5ccnametest",
		},

		{
			"Use incomplete command environment to return kerberos-wrapped command",
			&CommandEnvironment{
				CondorCreddHost:     "_condor_CREDD_HOST=foo",
				CondorCollectorHost: "_condor_COLLECTOR_HOST=bar",
				HtgettokenOpts:      "HTGETTOKENOPTS=baz",
			},
			"",
		},
	}

	cmdExecutable, err := exec.LookPath("true")
	if err != nil {
		t.Error("Could not find executable true to run tests")
		t.Fail()
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				cmd := KerberosEnvironmentWrappedCommand(context.Background(), test.environ, cmdExecutable)
				found := false
				for _, keyValue := range cmd.Env {
					if keyValue == test.expectedKrb5ccNameSetting {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"Could not find key-value pair %s in command environment",
						test.expectedKrb5ccNameSetting,
					)
				}
			},
		)
	}
}

// TestEnvironmentWrappedCommand makes sure that the CommandEnvironment we pass to EnvironmentWrappedCommand gives us the
// right command environment
func TestEnvironmentWrappedCommand(t *testing.T) {
	environ := &CommandEnvironment{
		Krb5ccname:          "KRB5CCNAME=krb5ccnametest",
		CondorCreddHost:     "_condor_CREDD_HOST=foo",
		CondorCollectorHost: "_condor_COLLECTOR_HOST=bar",
		HtgettokenOpts:      "HTGETTOKENOPTS=baz",
	}

	cmdExecutable, err := exec.LookPath("true")
	if err != nil {
		t.Error("Could not find executable true to run tests")
		t.Fail()
	}
	cmd := EnvironmentWrappedCommand(context.Background(), environ, cmdExecutable)

	environKeyValSlice := make([]string, 0)
	for _, field := range getAllSupportedCommandEnvironmentFields() {
		environKeyValSlice = append(environKeyValSlice, environ.GetSetting(field))
	}

	if ok := utils.IsSliceSubSlice(environKeyValSlice, cmd.Env); !ok {
		t.Errorf("Key-value pair in test environment not found in command environment: %s", err.Error())
	}
}
