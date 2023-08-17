// Package vaultToken provides functions for obtaining and validating Hashicorp vault tokens using the configured HTCondor installation
package vaultToken

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

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

// StoreAndValidateTokens stores a vault token in the passed in Hashicorp vault server and the passed in credd.
func StoreAndValidateTokens(ctx context.Context, serviceName, credd, vaultServer string, environ *environment.CommandEnvironment, interactive bool) error {
	funcLogger := log.WithFields(log.Fields{
		"serviceName": serviceName,
		"vaultServer": vaultServer,
		"credd":       credd,
	})
	if err := getTokensandStoreinVault(ctx, serviceName, credd, vaultServer, environ, interactive); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			funcLogger.Error("Context timeout")
			return err
		}
		funcLogger.Errorf("Could not obtain vault token: %s", err)
		return err
	}
	funcLogger.Debug("Stored vault and bearer tokens in vault and condor_credd/schedd")

	// Validate vault token
	vaultTokenFilename, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		funcLogger.Error("Could not get default vault token location")
		return err
	}

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		funcLogger.Error("Could not validate vault token")
		return err
	}

	funcLogger.Debug("Validated vault token")
	return nil
}

// TODO STILL UNDER DEVELOPMENT.  Export when ready
func GetToken(ctx context.Context, userPrincipal, serviceName, vaultServer string, environ environment.CommandEnvironment) error {
	htgettokenArgs := []string{
		"-d",
		"-a",
		vaultServer,
		"-i",
		serviceName,
	}

	htgettokenCmd := environment.EnvironmentWrappedCommand(ctx, &environ, vaultExecutables["htgettoken"], htgettokenArgs...)
	// TODO Get rid of all this when it works
	htgettokenCmd.Stdout = os.Stdout
	htgettokenCmd.Stderr = os.Stderr
	log.Debug(htgettokenCmd.Args)

	log.WithField("service", serviceName).Info("Running htgettoken to get vault and bearer tokens")
	if stdoutStderr, err := htgettokenCmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("service", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("service", serviceName).Errorf("Could not get vault token:\n%s", string(stdoutStderr[:]))
		return err
	}

	log.WithField("service", serviceName).Debug("Successfully got vault token")
	return nil
}

// TODO Unit test giving a bogus vault server
// getTokensandStoreinVault stores a refresh token in a configured Hashicorp vault and obtains vault and bearer tokens for the user.  If run
// using interactive=true, it will display stdout/stderr on the stdout of the caller
func getTokensandStoreinVault(ctx context.Context, serviceName, credd, vaultServer string, environ *environment.CommandEnvironment, interactive bool) error {
	funcLogger := log.WithFields(log.Fields{
		"service":     serviceName,
		"vaultServer": vaultServer,
		"credd":       credd,
	})

	// Store token in vault and get new vault token
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

	// Set _condor_CREDD_HOST and _condor_SEC_CREDENTIAL_GETTOKEN_OPTS in environment
	newEnv := environ.Copy()
	newEnv.SetCondorCreddHost(credd)
	oldCondorSecCredentialGettokenOpts := newEnv.GetValue(environment.CondorSecCredentialGettokenOpts)
	newEnv.SetCondorSecCredentialGettokenOpts(oldCondorSecCredentialGettokenOpts + fmt.Sprintf("-a %s", vaultServer))

	getTokensAndStoreInVaultCmd := environment.EnvironmentWrappedCommand(ctx, newEnv, vaultExecutables["condor_vault_storer"], cmdArgs...)

	funcLogger.Info("Storing and obtaining vault token")
	funcLogger.WithFields(log.Fields{
		"command":     getTokensAndStoreInVaultCmd.String(),
		"environment": newEnv.String(),
	}).Debug("Running command to store vault token")

	if interactive {
		// We need to capture stdout and stderr on the terminal so the user can authenticate
		getTokensAndStoreInVaultCmd.Stdout = os.Stdout
		getTokensAndStoreInVaultCmd.Stderr = os.Stderr

		if err := getTokensAndStoreInVaultCmd.Start(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				funcLogger.Error("Context timeout")
				return ctx.Err()
			}
			funcLogger.Errorf("Error starting condor_vault_storer command to store and obtain tokens; %s", err.Error())
		}
		if err := getTokensAndStoreInVaultCmd.Wait(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				funcLogger.Error("Context timeout")
				return ctx.Err()
			}
			funcLogger.Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
			return err
		}
	} else {
		if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				funcLogger.Error("Context timeout")
				return ctx.Err()
			}
			funcLogger.Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
			funcLogger.Errorf("%s", stdoutStderr)
			return err
		} else {
			if len(stdoutStderr) > 0 {
				funcLogger.WithField("environment", newEnv.String()).Debugf("%s", stdoutStderr)
			}
		}
	}
	return nil
}

// validateVaultToken verifies that a vault token (service token as Hashicorp calls them) indicated by the filename is valid
func validateVaultToken(vaultTokenFilename string) error {
	vaultTokenBytes, err := os.ReadFile(vaultTokenFilename)
	if err != nil {
		log.WithField("filename", vaultTokenFilename).Error("Could not read tokenfile for verification.")
		return err
	}

	vaultTokenString := string(vaultTokenBytes[:])

	if !IsServiceToken(vaultTokenString) {
		errString := "vault token failed validation"
		log.WithField("filename", vaultTokenFilename).Error(errString)
		return &InvalidVaultTokenError{
			vaultTokenFilename,
			errString,
		}
	}
	return nil
}

// Borrowed from hashicorp's vault API, since we ONLY need this func
// Source: https://github.com/hashicorp/vault/blob/main/vault/version_store.go
// and https://github.com/hashicorp/vault/blob/main/sdk/helper/consts/token_consts.go

const (
	ServiceTokenPrefix       = "hvs."
	LegacyServiceTokenPrefix = "s."
)

// IsServiceToken validates that a token string follows the Hashicorp service token convention
func IsServiceToken(token string) bool {
	return strings.HasPrefix(token, ServiceTokenPrefix) ||
		strings.HasPrefix(token, LegacyServiceTokenPrefix)
}

// InvalidVaultTokenError is an error that indicates that the token contained in filename is not a valid Hashicorp Service Token
// (what is called a vault token in the managed-tokens/OSG/WLCG world)
type InvalidVaultTokenError struct {
	filename string
	msg      string
}

func (i *InvalidVaultTokenError) Error() string {
	return fmt.Sprintf(
		"%s is an invalid vault/service token. %s",
		i.filename,
		i.msg,
	)
}

// GetAllVaultTokenLocations returns the locations of the vault tokens that both HTCondor and other OSG grid tools will use.
// The first element of the returned slice is the standard location for most grid tools, and the second is the standard for
// HTCondor
func GetAllVaultTokenLocations(serviceName string) ([]string, error) {
	vaultTokenLocations := make([]string, 0, 2)
	funcLogger := log.WithField("service", serviceName)

	defaultLocation, err := getDefaultVaultTokenLocation()
	if err != nil {
		funcLogger.Error("Could not get default vault location")
		return vaultTokenLocations, err
	}
	condorLocation, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		funcLogger.Error("Could not get condor vault location")
		return vaultTokenLocations, err
	}

	vaultTokenLocationsMap := map[string]struct{}{defaultLocation: {}, condorLocation: {}}
	nonExistentLocations := make([]string, 0, len(vaultTokenLocationsMap))

	// Check each location to make sure the file actually exists.  If not, remove from map
	for location := range vaultTokenLocationsMap {
		if _, err := os.Stat(location); errors.Is(err, os.ErrNotExist) {
			nonExistentLocations = append(nonExistentLocations, location)
		}
	}
	for _, location := range nonExistentLocations {
		delete(vaultTokenLocationsMap, location)
	}
	for location := range vaultTokenLocationsMap {
		vaultTokenLocations = append(vaultTokenLocations, location)
	}

	return vaultTokenLocations, nil
}

// RemoveServiceVaultTokens removes the vault token files at the standard OSG Grid Tools and HTCondor locations
func RemoveServiceVaultTokens(serviceName string) error {
	vaultTokenLocations, err := GetAllVaultTokenLocations(serviceName)
	if err != nil {
		log.WithField("service", serviceName).Error("Could not get vault token locations for deletion")
	}
	for _, vaultToken := range vaultTokenLocations {
		tokenLogger := log.WithFields(log.Fields{
			"service":  serviceName,
			"filename": vaultToken,
		})
		err := os.Remove(vaultToken)
		switch {
		case errors.Is(err, os.ErrNotExist):
			tokenLogger.Warn("Vault token not removed because the file does not exist")
		case err != nil:
			tokenLogger.Error("Could not remove vault token")
			return err
		default:
			tokenLogger.Debug("Removed vault token")
		}
	}
	return nil
}

// getCondorVaultTokenLocation returns the location of vault token that HTCondor uses based on the current user's UID
func getCondorVaultTokenLocation(serviceName string) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		log.WithField("service", serviceName).Error(err)
		return "", err
	}
	currentUID := currentUser.Uid
	return fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, serviceName), nil
}

// getDefaultVaultTokenLocation returns the location of vault token that most OSG grid tools use based on the current user's UID
func getDefaultVaultTokenLocation() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		log.Error(err)
		return "", err
	}
	currentUID := currentUser.Uid
	return fmt.Sprintf("/tmp/vt_u%s", currentUID), nil
}
