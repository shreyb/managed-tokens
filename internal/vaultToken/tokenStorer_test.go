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
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"testing"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/utils"
)

type MockTokenStorer struct {
	tokenStorerFunc func(context.Context, *environment.CommandEnvironment) error
	tokenValidator  func() error
}

func (t *MockTokenStorer) GetServiceName() string { return "mockService" }
func (t *MockTokenStorer) GetCredd() string       { return "mockCredd" }
func (t *MockTokenStorer) GetVaultServer() string { return "mockVaultServer" }
func (t *MockTokenStorer) validateToken() error {
	return t.tokenValidator()
}
func (t *MockTokenStorer) getTokensAndStoreInVault(ctx context.Context, environ *environment.CommandEnvironment) error {
	return t.tokenStorerFunc(ctx, environ)
}

func TestStoreAndValidateToken(t *testing.T) {
	type testCase struct {
		description string
		TokenStorer
		expectedErr error
	}

	storerError := errors.New("Error storing vault token")
	storerErrorAuthNeededTimeout := &ErrAuthNeeded{underlyingError: errHtgettokenTimeout}
	validatorError := errors.New("Error validating vault token")

	testCases := []testCase{
		{
			"No-error case",
			&MockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return nil },
				func() error { return nil },
			},
			nil,
		},
		{
			"Bad storer, good validator",
			&MockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return storerError },
				func() error { return nil },
			},
			storerError,
		},
		{
			"Good storer, bad validator",
			&MockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return nil },
				func() error { return validatorError },
			},
			validatorError,
		},
		{
			"Bad storer, bad validator",
			&MockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return storerError },
				func() error { return validatorError },
			},
			storerError,
		},
		{
			"Auth needed - timeout error, good validator",
			&MockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return storerErrorAuthNeededTimeout },
				func() error { return nil },
			},
			storerErrorAuthNeededTimeout,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				if err := StoreAndValidateToken(
					context.Background(),
					test.TokenStorer,
					&environment.CommandEnvironment{},
				); !errors.Is(test.expectedErr, err) {
					t.Errorf("Expected error %s.  Got %s", test.expectedErr, err)
				}
			},
		)

	}

}

func TestSetupCmdWithEnvironmentForTokenStorer(t *testing.T) {
	tokenStorer := &MockTokenStorer{}
	environ := new(environment.CommandEnvironment)

	expected := exec.CommandContext(context.Background(), vaultExecutables["condor_vault_storer"], tokenStorer.GetServiceName())
	result := setupCmdWithEnvironmentForTokenStorer(context.Background(), tokenStorer, environ)

	if result.Path != expected.Path {
		t.Errorf("Got wrong executable to run.  Expected %s, got %s", expected.Path, result.Path)
	}
	if !slices.Equal(expected.Args, result.Args) {
		t.Errorf("Got wrong command args.  Expected %v, got %v", expected.Args, result.Args)
	}

	checkEnvVars := []string{
		"_condor_CREDD_HOST=mockCredd",
		"_condor_SEC_CREDENTIAL_GETTOKEN_OPTS=-a mockVaultServer",
	}
	for _, checkEnv := range checkEnvVars {
		if !slices.Contains[[]string](result.Env, checkEnv) {
			t.Errorf("Result cmd does not have right environment variables.  Missing %s", checkEnv)
		}
	}
}

func TestGetCmdArgsForTokenStorer(t *testing.T) {
	serviceName := "testService"
	type testCase struct {
		description  string
		verboseSetup func() context.Context
		expectedArgs []string
	}

	testCases := []testCase{
		{
			"No verbose",
			func() context.Context { return context.Background() },
			[]string{serviceName},
		},
		{
			"Verbose",
			func() context.Context {
				return utils.ContextWithVerbose(context.Background())
			},
			[]string{"-v", serviceName},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				ctx := test.verboseSetup()
				if cmdArgs := getCmdArgsForTokenStorer(ctx, serviceName); !slices.Equal(cmdArgs, test.expectedArgs) {
					t.Errorf("cmdArgs slices are not equal. Expected %v, got %v", test.expectedArgs, cmdArgs)
				}
			},
		)
	}

}

func TestSetupEnvironmentForTokenStorer(t *testing.T) {
	credd := "test.credd"
	vaultServer := "test.vault.server"

	type testCase struct {
		description     string
		oldEnvfunc      func() *environment.CommandEnvironment
		expectedEnvfunc func() *environment.CommandEnvironment
	}

	testCases := []testCase{
		{
			"Empty original env and old _condor_SEC_CREDENTIAL_GETTOKEN_OPTS",
			func() *environment.CommandEnvironment { return new(environment.CommandEnvironment) },
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetCondorCreddHost(credd)
				env.SetCondorSecCredentialGettokenOpts(fmt.Sprintf("-a %s", vaultServer))
				return env
			},
		},
		{
			"Empty original env, filled old _condor_SEC_CREDENTIAL_GETTOKEN_OPTS",
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetCondorSecCredentialGettokenOpts("--foo bar")
				return env
			},
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetCondorCreddHost(credd)
				env.SetCondorSecCredentialGettokenOpts(fmt.Sprintf("--foo bar -a %s", vaultServer))
				return env
			},
		},
		{
			"Filled original env, empty old _condor_SEC_CREDENTIAL_GETTOKEN_OPTS",
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetKrb5ccname("blahblah", environment.FILE)
				return env
			},
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetKrb5ccname("blahblah", environment.FILE)
				env.SetCondorCreddHost(credd)
				env.SetCondorSecCredentialGettokenOpts(fmt.Sprintf("-a %s", vaultServer))
				return env
			},
		},
		{
			"Filled original env, filled old _condor_SEC_CREDENTIAL_GETTOKEN_OPTS",
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetKrb5ccname("blahblah", environment.FILE)
				env.SetCondorSecCredentialGettokenOpts("--foo bar")
				return env
			},
			func() *environment.CommandEnvironment {
				env := new(environment.CommandEnvironment)
				env.SetKrb5ccname("blahblah", environment.FILE)
				env.SetCondorCreddHost(credd)
				env.SetCondorSecCredentialGettokenOpts(fmt.Sprintf("--foo bar -a %s", vaultServer))
				return env
			},
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				oldEnv := test.oldEnvfunc()
				expectedEnv := test.expectedEnvfunc()
				if resultEnv := setupEnvironmentForTokenStorer(oldEnv, credd, vaultServer); *resultEnv != *expectedEnv {
					t.Errorf("Did not get expected environmnent.  Expected %s, got %s", expectedEnv.String(), resultEnv.String())
				}
			},
		)
	}
}

func TestCheckStdOutForErrorAuthNeeded(t *testing.T) {
	type testCase struct {
		description          string
		stdoutStderr         []byte
		expectedErrTypeCheck error
		expectedWrappedError error
	}

	testCases := []testCase{
		{
			"Random string - should not find result",
			[]byte("This is a random string"),
			nil,
			nil,
		},
		{
			"Auth needed",
			[]byte("Authentication needed for myservice"),
			&ErrAuthNeeded{},
			nil,
		},
		{
			"Auth needed - timeout",
			[]byte("Authentication needed for myservice\n\n\nblahblah\n\nhtgettoken: Polling for response took longer than 2 minutes"),
			&ErrAuthNeeded{},
			errHtgettokenTimeout,
		},
		{
			"Auth needed - permission denied",
			[]byte("Authentication needed for myservice\n\n\nblahblah\n\nhtgettoken: blahblah HTTP Error 403: Forbidden: permission denied"),
			&ErrAuthNeeded{},
			errHtgettokenPermissionDenied,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				err := checkStdoutStderrForAuthNeededError(test.stdoutStderr)
				if err == nil && test.expectedErrTypeCheck == nil {
					return
				}
				var err1 *ErrAuthNeeded
				if !errors.As(err, &err1) {
					t.Errorf("Expected returned error to be of type *errAuthNeeded.  Got %T instead", err)
					return
				}

				if errVal := errors.Unwrap(err); !errors.Is(errVal, test.expectedWrappedError) {
					t.Errorf("Did not get expected wrapped error.  Expected error %v to be wrapped, but full error is %v.", test.expectedWrappedError, err)
				}

			},
		)
	}
}
