package vaultToken

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/utils"
)

var vaultExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
	"condor_status":       "",
	"htgettoken":          "",
}

func init() {
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", fmt.Sprintf("%s:/usr/bin:/usr/sbin", oldPath))
	if err := utils.CheckForExecutables(vaultExecutables); err != nil {
		log.WithField("PATH", os.Getenv("PATH")).Fatal("Could not find path to condor executables")
	}
}

// TokenStorer contains the methods needed to store a vault token in the condor credd and a hashicorp vault.  It should be passed into
// StoreAndValidateTokens so that any token that is stored is also validated
type TokenStorer interface {
	getServiceName() string
	getCredd() string
	getVaultServer() string
	getTokensAndStoreInVault(context.Context, *environment.CommandEnvironment) error
	validateToken() error
}

// StoreAndValidateToken stores a vault token in the passed in Hashicorp vault server and the passed in credd.
func StoreAndValidateToken(ctx context.Context, t TokenStorer, environ *environment.CommandEnvironment) error {
	funcLogger := log.WithFields(log.Fields{
		"serviceName": t.getServiceName(),
		"vaultServer": t.getVaultServer(),
		"credd":       t.getCredd(),
	})
	if err := t.getTokensAndStoreInVault(ctx, environ); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return err
		}
		funcLogger.Errorf("Could not obtain vault token: %s", err)
		return err
	}
	funcLogger.Debug("Stored vault and bearer tokens in vault and condor_credd/schedd")

	if err := t.validateToken(); err != nil {
		funcLogger.Error("Could not validate vault token for TokenStorer")
		return err
	}

	funcLogger.Debug("Validated vault token")
	return nil
}

// InteractiveTokenStorer is a type to use when it is anticipated that the token storing action will require user interaction
type InteractiveTokenStorer struct {
	serviceName string
	credd       string
	vaultServer string
}

func NewInteractiveTokenStorer(serviceName, credd, vaultServer string) *InteractiveTokenStorer {
	return &InteractiveTokenStorer{
		serviceName: serviceName,
		credd:       credd,
		vaultServer: vaultServer,
	}
}

func (t *InteractiveTokenStorer) getServiceName() string { return t.serviceName }
func (t *InteractiveTokenStorer) getCredd() string       { return t.credd }
func (t *InteractiveTokenStorer) getVaultServer() string { return t.vaultServer }
func (t *InteractiveTokenStorer) validateToken() error {
	return validateServiceVaultToken(t.serviceName)
}

// getTokensandStoreinVault stores a refresh token in a configured Hashicorp vault and obtains vault and bearer tokens for the user.
// It allows for the token-storing command to prompt the user for action
func (t *InteractiveTokenStorer) getTokensAndStoreInVault(ctx context.Context, environ *environment.CommandEnvironment) error {
	funcLogger := log.WithFields(log.Fields{
		"service":     t.serviceName,
		"vaultServer": t.vaultServer,
		"credd":       t.credd,
	})
	getTokensAndStoreInVaultCmd := setupCmdWithEnvironmentForTokenStorer(ctx, t, environ)

	// We need to capture stdout and stderr on the terminal so the user can authenticate
	getTokensAndStoreInVaultCmd.Stdout = os.Stdout
	getTokensAndStoreInVaultCmd.Stderr = os.Stderr

	if err := getTokensAndStoreInVaultCmd.Start(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		funcLogger.Errorf("Error starting condor_vault_storer command to store and obtain tokens; %s", err.Error())
		return err
	}
	if err := getTokensAndStoreInVaultCmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		funcLogger.Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
		return err
	}
	return nil
}

// NonInteractiveTokenStorer is a type to use when it is anticipated that the token storing action will not require user interaction
type NonInteractiveTokenStorer struct {
	serviceName string
	credd       string
	vaultServer string
}

func NewNonInteractiveTokenStorer(serviceName, credd, vaultServer string) *NonInteractiveTokenStorer {
	return &NonInteractiveTokenStorer{
		serviceName: serviceName,
		credd:       credd,
		vaultServer: vaultServer,
	}
}

func (t *NonInteractiveTokenStorer) getServiceName() string { return t.serviceName }
func (t *NonInteractiveTokenStorer) getCredd() string       { return t.credd }
func (t *NonInteractiveTokenStorer) getVaultServer() string { return t.vaultServer }
func (t *NonInteractiveTokenStorer) validateToken() error {
	return validateServiceVaultToken(t.serviceName)
}

// getTokensandStoreinVault stores a refresh token in a configured Hashicorp vault and obtains vault and bearer tokens for the user.
// It doew not allow for the token-storing command to prompt the user for action
func (t *NonInteractiveTokenStorer) getTokensAndStoreInVault(ctx context.Context, environ *environment.CommandEnvironment) error {
	funcLogger := log.WithFields(log.Fields{
		"service":     t.serviceName,
		"vaultServer": t.vaultServer,
		"credd":       t.credd,
	})
	getTokensAndStoreInVaultCmd := setupCmdWithEnvironmentForTokenStorer(ctx, t, environ)

	// For non-interactive use, it is an error condition if the user is prompted to authenticate, so we want to just run the command
	// and capture the output
	if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return ctx.Err()
		}
		authErr := checkStdoutStderrForAuthNeededError(stdoutStderr)
		funcLogger.Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
		funcLogger.Errorf("%s", stdoutStderr)
		if authErr != nil {
			return authErr
		}
		return err
	} else {
		if len(stdoutStderr) > 0 {
			funcLogger.Debugf("%s", stdoutStderr)
		}
	}
	return nil
}

func setupCmdWithEnvironmentForTokenStorer(ctx context.Context, t TokenStorer, environ *environment.CommandEnvironment) *exec.Cmd {
	funcLogger := log.WithFields(log.Fields{
		"service":     t.getServiceName(),
		"vaultServer": t.getVaultServer(),
		"credd":       t.getCredd(),
	})
	cmdArgs := getCmdArgsForTokenStorer(ctx, t.getServiceName())
	newEnv := setupEnvironmentForTokenStorer(environ, t.getCredd(), t.getVaultServer())
	getTokensAndStoreInVaultCmd := environment.EnvironmentWrappedCommand(ctx, newEnv, vaultExecutables["condor_vault_storer"], cmdArgs...)

	funcLogger.Info("Storing and obtaining vault token")
	funcLogger.WithFields(log.Fields{
		"command":     getTokensAndStoreInVaultCmd.String(),
		"environment": newEnv.String(),
	}).Debug("Command to store vault token")

	return getTokensAndStoreInVaultCmd
}

func getCmdArgsForTokenStorer(ctx context.Context, serviceName string) []string {
	funcLogger := log.WithField("service", serviceName)
	cmdArgs := make([]string, 0, 2)
	verbose, err := utils.GetVerboseFromContext(ctx)
	// If err == utils.ErrContextKeyNotStored, don't worry about it - we just use the default verbose value of false
	if !errors.Is(err, utils.ErrContextKeyNotStored) && err != nil {
		funcLogger.Error("Could not retrieve verbose setting from context.  Setting verbose to false")
	}
	funcLogger.Debugf("Verbose is set to %t", verbose)
	if verbose {
		cmdArgs = append(cmdArgs, "-v")
	}
	cmdArgs = append(cmdArgs, serviceName)
	return cmdArgs
}

// setupEnvironmentForTokenStorer sets _condor_CREDD_HOST and _condor_SEC_CREDENTIAL_GETTOKEN_OPTS in a new environment for condor_vault_storer
func setupEnvironmentForTokenStorer(environ *environment.CommandEnvironment, credd string, vaultServer string) *environment.CommandEnvironment {
	newEnv := environ.Copy()
	newEnv.SetCondorCreddHost(credd)
	oldCondorSecCredentialGettokenOpts := newEnv.GetValue(environment.CondorSecCredentialGettokenOpts)
	var maybeSpace string
	if oldCondorSecCredentialGettokenOpts != "" {
		maybeSpace = " "
	}
	newEnv.SetCondorSecCredentialGettokenOpts(oldCondorSecCredentialGettokenOpts + maybeSpace + fmt.Sprintf("-a %s", vaultServer))
	return newEnv
}

func checkStdoutStderrForAuthNeededError(stdoutStderr []byte) error {
	authNeededRegexp := regexp.MustCompile(`Authentication needed for.*`)
	if !authNeededRegexp.Match(stdoutStderr) {
		return nil
	}

	errToReturn := &ErrAuthNeeded{}
	htgettokenTimeoutRegexp := regexp.MustCompile(`htgettoken: Polling for response took longer than.*`)
	if htgettokenTimeoutRegexp.Match(stdoutStderr) {
		errToReturn.underlyingError = errHtgettokenTimeout
	}
	htgettokenPermissionDeniedRegexp := regexp.MustCompile(`htgettoken:.*403.*permission denied`)
	if htgettokenPermissionDeniedRegexp.Match(stdoutStderr) {
		errToReturn.underlyingError = errHtgettokenPermissionDenied
	}

	return errToReturn
}

type ErrAuthNeeded struct {
	underlyingError error
}

func (e *ErrAuthNeeded) Error() string {
	msg := "authentication needed for service to generate vault token"
	if e.underlyingError != nil {
		msg = fmt.Sprintf("%s: %s", msg, e.underlyingError.Error())
	}
	return msg
}

func (e *ErrAuthNeeded) Unwrap() error { return e.underlyingError }

var (
	errHtgettokenTimeout          = errors.New("htgettoken timeout to generate vault token")
	errHtgettokenPermissionDenied = errors.New("permission denied to generate vault token")
)
