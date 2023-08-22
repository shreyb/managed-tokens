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

type mockTokenStorer struct {
	tokenStorerFunc func(context.Context, *environment.CommandEnvironment) error
	tokenValidator  func() error
}

func (t *mockTokenStorer) getServiceName() string { return "mockService" }
func (t *mockTokenStorer) getCredd() string       { return "mockCredd" }
func (t *mockTokenStorer) getVaultServer() string { return "mockVaultServer" }
func (t *mockTokenStorer) validateToken() error {
	return t.tokenValidator()
}
func (t *mockTokenStorer) getTokensAndStoreInVault(ctx context.Context, environ *environment.CommandEnvironment) error {
	return t.tokenStorerFunc(ctx, environ)
}

func TestStoreAndValidateToken(t *testing.T) {
	type testCase struct {
		description string
		TokenStorer
		expectedErr error
	}

	storerError := errors.New("Error storing vault token")
	validatorError := errors.New("Error validating vault token")

	testCases := []testCase{
		{
			"No-error case",
			&mockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return nil },
				func() error { return nil },
			},
			nil,
		},
		{
			"Bad storer, good validator",
			&mockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return storerError },
				func() error { return nil },
			},
			storerError,
		},
		{
			"Good storer, bad validator",
			&mockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return nil },
				func() error { return validatorError },
			},
			validatorError,
		},
		{
			"Bad storer, bad validator",
			&mockTokenStorer{
				func(context.Context, *environment.CommandEnvironment) error { return storerError },
				func() error { return validatorError },
			},
			storerError,
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
	tokenStorer := &mockTokenStorer{}
	environ := new(environment.CommandEnvironment)

	expected := exec.CommandContext(context.Background(), vaultExecutables["condor_vault_storer"], tokenStorer.getServiceName())
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
