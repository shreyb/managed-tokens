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
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/tracing"
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

type serviceGetter interface {
	getService() string
}

type serviceString string

func (s serviceString) getService() string { return string(s) }

type uidGetter interface {
	getUID() string
}

type uidGetterUser user.User

func (u uidGetterUser) getUID() string { return u.Uid }

// GetAllVaultTokenLocations returns the locations of the vault tokens that both HTCondor and other OSG grid tools will use.
// The first element of the returned slice is the standard location for most grid tools, and the second is the standard for
// HTCondor
func GetAllVaultTokenLocations(serviceName string) ([]string, error) {
	s := serviceString(serviceName)
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("could not get current user to determine vault token locations: %w", err)
	}
	u := uidGetterUser(*currentUser)
	return getAllVaultTokenLocations(u, s)
}

// getAllVaultTokenLocations returns the locations of the vault tokens that both HTCondor and other OSG grid tools will use.
// The first element of the returned slice is the standard location for most grid tools, and the second is the standard for
// HTCondor
func getAllVaultTokenLocations(u uidGetter, s serviceGetter) ([]string, error) {
	vaultTokenLocations := make([]string, 0, 2)
	errs := make([]error, 0, 2)

	defaultLocation := getDefaultVaultTokenLocation(u)
	if _, err := os.Stat(defaultLocation); err != nil { // Check to see if the file exists and we can read it
		errs = append(errs, fmt.Errorf("could not get vault token default location: %w", err))
	} else {
		vaultTokenLocations = append(vaultTokenLocations, defaultLocation)
	}

	condorLocation := getCondorVaultTokenLocation(u, s)
	if _, err := os.Stat(condorLocation); err != nil { // Check to see if the file exists and we can read it
		errs = append(errs, fmt.Errorf("could not get vault token condor location: %w", err))
	} else {
		vaultTokenLocations = append(vaultTokenLocations, condorLocation)
	}

	if len(errs) > 0 {
		var underlying error
		for _, err := range errs {
			if !errors.Is(err, os.ErrNotExist) {
				underlying = err
				break
			}
		}
		if underlying == nil {
			return vaultTokenLocations, &ErrGetTokenLocation{ // all errors were os.ErrNotExist
				ServiceName: s.getService(),
				underlying:  fmt.Errorf("could not remove all vault tokens: %w", os.ErrNotExist),
			}
		}
		return nil, &ErrGetTokenLocation{
			ServiceName: s.getService(),
			underlying:  fmt.Errorf("could not remove all vault tokens: %w", underlying),
		}
	}

	return vaultTokenLocations, nil
}

// RemoveServiceVaultTokens removes the vault token files at the standard OSG Grid Tools and HTCondor locations
func RemoveServiceVaultTokens(serviceName string) error {
	s := serviceString(serviceName)
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("could not get current user to remove vault tokens: %w", err)
	}
	u := uidGetterUser(*currentUser)
	return removeServiceVaultTokens(u, s)
}

// RemoveServiceVaultTokens removes the vault token files at the standard OSG Grid Tools and HTCondor locations
func removeServiceVaultTokens(u uidGetter, s serviceGetter) error {
	errGetTokenPathsNotExist := false
	vaultTokenLocations, err := getAllVaultTokenLocations(u, s)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not get vault token locations for deletion: %w", err)
		}
		errGetTokenPathsNotExist = true
	}

	if len(vaultTokenLocations) == 0 {
		return fmt.Errorf("no vault token files to delete: %w", os.ErrNotExist)
	}

	errs := make([]error, 0, len(vaultTokenLocations))
	for _, vaultToken := range vaultTokenLocations {
		if err := os.Remove(vaultToken); err != nil {
			errs = append(errs, fmt.Errorf("could not remove token at path %s: %w", vaultToken, err))
		}
	}

	if len(errs) > 0 {
		var underlying error
		for _, err := range errs {
			if !errors.Is(err, os.ErrNotExist) {
				underlying = err
				break
			}
		}
		if underlying == nil { // All errors were os.ErrNotExist
			return &ErrTokenRemove{
				ServiceName: s.getService(),
				underlying:  fmt.Errorf("could not remove all vault tokens: %w", os.ErrNotExist),
			}
		}
		return &ErrTokenRemove{
			ServiceName: s.getService(),
			underlying:  fmt.Errorf("could not remove all vault tokens: %w", underlying),
		}

	}

	// Even though all of our attempted removes went well, we want the caller to know that
	// when we tried to make sure that the paths of the expected token locations existed,
	// at least one of them returned an os.ErrNotExist.
	if errGetTokenPathsNotExist {
		return fmt.Errorf("could not remove at least one vault token: %w", os.ErrNotExist)
	}

	return nil
}

// getCondorVaultTokenLocation returns the location of vault token that HTCondor uses based on the current user's UID
func getCondorVaultTokenLocation(u uidGetter, s serviceGetter) string {
	return filepath.Join("/tmp", fmt.Sprintf("vt_u%s-%s", u.getUID(), s.getService()))
}

// getDefaultVaultTokenLocation returns the location of vault token that most OSG grid tools use based on the current user's UID
func getDefaultVaultTokenLocation(u uidGetter) string {
	return filepath.Join("/tmp", fmt.Sprintf("vt_u%s", u.getUID()))
}

func validateServiceVaultToken(u uidGetter, s serviceGetter) error {
	vaultTokenFilename := getCondorVaultTokenLocation(u, s)

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		return fmt.Errorf("could not validate vault token: %w", err)
	}
	return nil
}

// validateVaultToken verifies that a vault token (service token as Hashicorp calls them) indicated by the filename is valid
func validateVaultToken(vaultTokenFilename string) error {
	vaultTokenBytes, err := os.ReadFile(vaultTokenFilename)
	if err != nil {
		return fmt.Errorf("could not read tokenfile %s for verification: %w", vaultTokenFilename, err)
	}

	vaultTokenString := string(vaultTokenBytes[:])
	if !IsServiceToken(vaultTokenString) {
		return &InvalidVaultTokenError{
			vaultTokenFilename,
			"vault token failed validation",
		}
	}
	return nil
}

// TODO STILL UNDER DEVELOPMENT.  Export when ready, and add tracing
func GetToken(ctx context.Context, userPrincipal, serviceName, vaultServer string, environ environment.CommandEnvironment) error {
	ctx, span := otel.GetTracerProvider().Tracer("managed-tokens").Start(ctx, "vaultToken.GetToken")
	// TODO Add attributes to span

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

	if stdoutStderr, err := htgettokenCmd.CombinedOutput(); err != nil {
		err = fmt.Errorf("could not get vault token: %s: %w", string(stdoutStderr[:]), err)
		tracing.LogErrorWithTrace(span, err)
		return err
	}

	return nil
}

type ErrTokenRemove struct {
	ServiceName, FileName string
	underlying            error
}

func (e *ErrTokenRemove) Error() string {
	return fmt.Sprintf("could not remove token for service %s at path %s: %s", e.ServiceName, e.FileName, e.underlying)
}

func (e *ErrTokenRemove) Unwrap() error {
	return e.underlying
}

type ErrGetTokenLocation struct {
	ServiceName string
	underlying  error
}

func (e *ErrGetTokenLocation) Error() string {
	return fmt.Sprintf("could not get token location for service %s: %s", e.ServiceName, e.underlying)
}

func (e *ErrGetTokenLocation) Unwrap() error {
	return e.underlying
}
