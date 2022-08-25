package vaultToken

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/environment"
	"github.com/shreyb/managed-tokens/kerberos"
	"github.com/shreyb/managed-tokens/utils"
)

var vaultExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
	"htgettoken":          "",
}

func init() {
	os.Setenv("PATH", "/usr/bin:/usr/sbin")
	if err := utils.CheckForExecutables(vaultExecutables); err != nil {
		log.Fatal("Could not find path to condor executables")
	}
}

func StoreAndGetTokens(ctx context.Context, serviceName, userPrincipal string, environment environment.CommandEnvironment, interactive bool) error {
	// kswitch
	if err := kerberos.SwitchCache(ctx, userPrincipal, environment); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("serviceName", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("serviceName", serviceName).Error("Could not switch kerberos caches")
		return err
	}

	// Get token and store it in vault
	if err := getTokensandStoreinVault(ctx, serviceName, environment, interactive); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("serviceName", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("serviceName", serviceName).Error("Could not obtain vault token")
		return err
	}

	// Validate vault token
	vaultTokenFilename, err := getDefaultVaultTokenLocation(serviceName)
	if err != nil {
		log.WithField("service", serviceName).Error("Could not get default vault token location")
		return err
	}

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		log.WithField("service", serviceName).Error("Could not validate vault token")
		return err
	}

	// TODO Make this a debug
	log.WithField("service", serviceName).Info("Validated vault token")
	return nil
}

func GetToken(ctx context.Context, serviceName, userPrincipal, vaultServer string, environ environment.CommandEnvironment) error {
	if err := kerberos.SwitchCache(ctx, userPrincipal, environ); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.WithField("service", serviceName).Error("Context timeout")
			return ctx.Err()
		}
		log.WithField("service", serviceName).Error("Could not switch kerberos caches")
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

	log.WithField("service", serviceName).Info("Successfully got vault token")
	return nil
}

func getTokensandStoreinVault(ctx context.Context, serviceName string, environ environment.CommandEnvironment, interactive bool) error {
	// Store token in vault and get new vault token
	//TODO if verbose, add the -v flag here
	getTokensAndStoreInVaultCmd := environment.EnvironmentWrappedCommand(ctx, &environ, vaultExecutables["condor_vault_storer"], serviceName)

	log.WithField("service", serviceName).Info("Storing and obtaining vault token")

	if interactive {
		// We need to capture stdout and stderr on the terminal so the user can authenticate
		getTokensAndStoreInVaultCmd.Stdout = os.Stdout
		getTokensAndStoreInVaultCmd.Stderr = os.Stderr

		if err := getTokensAndStoreInVaultCmd.Start(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithField("service", serviceName).Errorf("Error starting condor_vault_storer command to store and obtain tokens; %s", err.Error())
		}
		if err := getTokensAndStoreInVaultCmd.Wait(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithField("service", serviceName).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err.Error())
			return err
		}
	} else {
		if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("service", serviceName).Error("Context timeout")
				return ctx.Err()
			}
			log.WithField("service", serviceName).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err.Error())
			log.WithField("service", serviceName).Errorf("%s", stdoutStderr)
			return err
		} else {
			log.WithField("service", serviceName).Infof("%s", stdoutStderr)
		}
	}

	log.WithField("service", serviceName).Info("Successfully obtained and stored vault token")

	return nil
}

// validateVaultToken verifies that a vault token (service token as Hashicorp calls them) indicated by the filename is valid
func validateVaultToken(vaultTokenFilename string) error {
	vaultTokenBytes, err := ioutil.ReadFile(vaultTokenFilename)
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

func IsServiceToken(token string) bool {
	return strings.HasPrefix(token, ServiceTokenPrefix) ||
		strings.HasPrefix(token, LegacyServiceTokenPrefix)
}

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

func getDefaultVaultTokenLocation(serviceName string) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", err
	}
	currentUID := currentUser.Uid
	return fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, serviceName), nil
}
