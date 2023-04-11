// Package vaultToken provides functions for obtaining and validating Hashicorp vault tokens using the configured HTCondor installation
package vaultToken

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/environment"
	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/utils"
)

var vaultExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
	"condor_status":       "",
	"htgettoken":          "",
}

func init() {
	os.Setenv("PATH", "/usr/bin:/usr/sbin")
	if err := utils.CheckForExecutables(vaultExecutables); err != nil {
		log.Fatal("Could not find path to condor executables")
	}
}

// Func to get all schedds - DONE
// In StoreAndGetTokens and other methods, copy environments so that we have one per schedd, then run - DONE
// condor_vault_storer on each - DONE
// Modify other funcs like that too here
// TODO: Maybe a context value to store condor_status constraint so we don't have to hard-code jobsub_lite stuff in there?
// Have anything that sets _condor_CREDD_HOST in the environment struct not actually set that unless
// in the config as an override
// Get Schedds at top level (main), then pass those down in worker.Configs.  Those can be passed down into StoreAndGetTokens

// StoreAndGetTokens will store a refresh token on the condor-configured vault server and obtain vault and bearer tokens for a service using HTCondor executables.
// It will also store the vault and bearer token in the condor_credd that resides on each schedd that is passed in with the schedds slice.
// Finally, it will validate the obtained vault token using the vault token pattern expected by Hashicorp (called a Service token by Hashicorp).
// If run in interactive mode, then the stdout/stderr will be displayed in the user's terminal.  This can be used, for example, if it is expected
// that the user might have to authenticate to the vault server.
func StoreAndGetTokens(ctx context.Context, userPrincipal, serviceName string, schedds []string, environ environment.CommandEnvironment, interactive bool) error {
	// kswitch
	if err := kerberos.SwitchCache(ctx, userPrincipal, environ); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("serviceName", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("serviceName", serviceName).Errorf("Could not switch kerberos caches: %s", err)
		return err
	}

	// If we're running on a cluster with multiple schedds, create CommandEnvironments for each so we store tokens in all the possible credds
	environmentsForCommands := make([]environment.CommandEnvironment, 0)
	for _, schedd := range schedds {
		newEnv := environ
		newEnv.CondorCreddHost = fmt.Sprintf("%s=%s", newEnv.ToEnvs()["CondorCreddHost"], schedd)
		environmentsForCommands = append(environmentsForCommands, newEnv)
	}

	// Listener for all of the getTokensAndStoreInVault goroutines
	var isError bool
	errChan := make(chan error)
	go func() {
		for err := range errChan {
			if err != nil && !isError {
				isError = true
			}
		}
	}()

	// Get token and store it in vault
	var wg sync.WaitGroup
	for _, environmentForCommand := range environmentsForCommands {
		wg.Add(1)
		go func(environmentForCommand environment.CommandEnvironment) {
			defer wg.Done()
			if err := getTokensandStoreinVault(ctx, serviceName, environmentForCommand, interactive); err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					log.WithField("serviceName", serviceName).Error("Context timeout")
					errChan <- ctx.Err()
				}
				log.WithFields(log.Fields{
					"serviceName": serviceName,
					"credd":       environmentForCommand.ToValues()["CondorCreddHost"],
				}).Errorf("Could not obtain vault token: %s", err)
				errChan <- err
				return
			}
			log.WithFields(log.Fields{
				"serviceName": serviceName,
				"credd":       environmentForCommand.ToValues()["CondorCreddHost"],
			}).Debug("Stored vault and bearer tokens in vault and condor_credd/schedd")
			errChan <- nil
		}(environmentForCommand)
	}
	wg.Wait()
	close(errChan)

	if isError {
		msg := "Error obtaining and/or storing vault tokens"
		log.WithField("serviceName", serviceName).Errorf(msg)
		return errors.New(msg)
	}

	// Validate vault token
	vaultTokenFilename, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		log.WithField("service", serviceName).Error("Could not get default vault token location")
		return err
	}

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		log.WithField("service", serviceName).Error("Could not validate vault token")
		return err
	}

	log.WithField("service", serviceName).Debug("Validated vault token")
	return nil
}

// TODO STILL UNDER DEVELOPMENT.  Export when ready
func GetToken(ctx context.Context, userPrincipal, serviceName, vaultServer string, environ environment.CommandEnvironment) error {
	if err := kerberos.SwitchCache(ctx, userPrincipal, environ); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("service", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("service", serviceName).Errorf("Could not switch kerberos caches: %s", err)
		return err
	}

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

// getTokensandStoreinVault stores a refresh token in a configured Hashicorp vault and obtains vault and bearer tokens for the user.  If run
// using interactive=true, it will display stdout/stderr on the stdout of the caller
func getTokensandStoreinVault(ctx context.Context, serviceName string, environ environment.CommandEnvironment, interactive bool) error {
	// Store token in vault and get new vault token
	cmdArgs := make([]string, 0, 2)
	verbose, ok := utils.GetVerboseFromContext(ctx)
	if !ok {
		log.Debug("Could not retrieve verbose setting from context, either because it was not set or because of an error.  Setting verbose to false")
		verbose = false
	}
	log.Debugf("Verbose is set to %t", verbose)

	if verbose {
		cmdArgs = append(cmdArgs, "-v")
	}
	cmdArgs = append(cmdArgs, serviceName)

	getTokensAndStoreInVaultCmd := environment.EnvironmentWrappedCommand(ctx, &environ, vaultExecutables["condor_vault_storer"], cmdArgs...)

	log.WithField("service", serviceName).Info("Storing and obtaining vault token")
	log.WithFields(log.Fields{
		"service":     serviceName,
		"command":     getTokensAndStoreInVaultCmd.String(),
		"environment": environ.String(),
	}).Debug("Running command to store vault token")

	if interactive {
		// We need to capture stdout and stderr on the terminal so the user can authenticate
		getTokensAndStoreInVaultCmd.Stdout = os.Stdout
		getTokensAndStoreInVaultCmd.Stderr = os.Stderr

		if err := getTokensAndStoreInVaultCmd.Start(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithFields(log.Fields{
				"service": serviceName,
				"credd":   environ.ToValues()["CondorCreddHost"],
			}).Errorf("Error starting condor_vault_storer command to store and obtain tokens; %s", err.Error())
		}
		if err := getTokensAndStoreInVaultCmd.Wait(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithFields(log.Fields{
				"service": serviceName,
				"credd":   environ.ToValues()["CondorCreddHost"],
			}).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
			return err
		}
	} else {
		if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithFields(log.Fields{
				"service": serviceName,
				"credd":   environ.ToValues()["CondorCreddHost"],
			}).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err)
			log.WithFields(log.Fields{
				"service": serviceName,
				"credd":   environ.ToValues()["CondorCreddHost"],
			}).Errorf("%s", stdoutStderr)
			return err
		} else {
			if len(stdoutStderr) > 0 {
				log.WithFields(log.Fields{
					"service":     serviceName,
					"environment": environ.String(),
					"credd":       environ.ToValues()["CondorCreddHost"],
				}).Debugf("%s", stdoutStderr)
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

	defaultLocation, err := getDefaultVaultTokenLocation()
	if err != nil {
		log.Error("Could not get default vault location")
		return vaultTokenLocations, err
	}
	condorLocation, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		log.Error("Could not get condor vault location")
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
		err := os.Remove(vaultToken)
		switch {
		case errors.Is(err, os.ErrNotExist):
			log.WithFields(log.Fields{
				"service":  serviceName,
				"filename": vaultToken,
			}).Warn("Vault token not removed because the file does not exist")
		case err != nil:
			log.WithFields(log.Fields{
				"service":  serviceName,
				"filename": vaultToken,
			}).Error("Could not remove vault token")
			return err
		default:
			log.WithFields(log.Fields{
				"service":  serviceName,
				"filename": vaultToken,
			}).Debug("Removed vault token")
		}
	}
	return nil
}

// getCondorVaultTokenLocation returns the location of vault token that HTCondor uses based on the current user's UID
func getCondorVaultTokenLocation(serviceName string) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		log.Error(err)
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
