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

	"github.com/fermitools/managed-tokens/internal/environment"
)

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
	funcLogger := log.WithField("service", serviceName)
	vaultTokenLocations := make([]string, 0, 2)

	defaultLocation, err := getDefaultVaultTokenLocation()
	if err != nil {
		funcLogger.Error("Could not get default vault location")
		return nil, err
	}
	if _, err := os.Stat(defaultLocation); err == nil { // Check to see if the file exists and we can read it
		vaultTokenLocations = append(vaultTokenLocations, defaultLocation)
	}

	condorLocation, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		funcLogger.Error("Could not get condor vault location")
		return nil, err
	}
	if _, err := os.Stat(condorLocation); err == nil { // Check to see if the file exists and we can read it
		vaultTokenLocations = append(vaultTokenLocations, condorLocation)
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
		if err := os.Remove(vaultToken); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				tokenLogger.Info("Vault token not removed because the file does not exist")
			} else {
				tokenLogger.Error("Could not remove vault token")
				return err
			}
		} else {
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

func validateServiceVaultToken(serviceName string) error {
	funcLogger := log.WithField("service", serviceName)
	vaultTokenFilename, err := getCondorVaultTokenLocation(serviceName)
	if err != nil {
		funcLogger.Error("Could not get default vault token location")
		return err
	}

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		funcLogger.Error("Could not validate vault token")
		return err
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
