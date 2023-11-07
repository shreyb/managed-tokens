package worker

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"time"

	log "github.com/sirupsen/logrus"
)

// These are functions that deal with staging and storing credd-specific vault tokens

// backupCondorVaultToken looks in the standard HTCondor location for a vault token.  If it finds
// one there, it will create a backup copy.  It returns a function that will restore the backup
// copy to the standard HTCondor location.  This returned func should ideally be defer called
// by the caller.
func backupCondorVaultToken(serviceName string) (restorePriorTokenFunc func() error, retErr error) {
	funcLogger := log.WithField("service", serviceName)

	// Default value for return func
	restorePriorTokenFunc = func() error { return nil }

	// Check for token at condorVaultTokenLocation, and move it out if needed
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)
	// TODO Strengthen this file check.  One more func that checks if a file exists or not, maybe goes into utils
	if _, err := os.Stat(condorVaultTokenLocation); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			funcLogger.Errorf("Could not stat condor vault token file that exists: %s", err)
			return restorePriorTokenFunc, err
		}
		// We had a vault token at condorVaultTokenLocation.  Move it to a temp file for now
		previousTokenTempFile, err := os.CreateTemp(os.TempDir(), "managed_tokens_condor_vault_token")
		if err != nil {
			funcLogger.Error("Could not create temp file for old token file")
			return restorePriorTokenFunc, err
		}
		funcLogger.Debugf("condor vault token already exists at %s.  Moving to temp location %s", condorVaultTokenLocation, previousTokenTempFile.Name())
		// TODO:  Think about how to test this
		if err := os.Rename(condorVaultTokenLocation, previousTokenTempFile.Name()); err != nil {
			funcLogger.Error("Could not move currently-existing condor vault token to staging location")
			return restorePriorTokenFunc, err
		}
		restorePriorTokenFunc = func() error {
			// TODO:  This part is not tested.  Think about how to do that
			if err := os.Rename(previousTokenTempFile.Name(), condorVaultTokenLocation); err != nil {
				// Create location in os.TempDir() that is stamped for possible later retrieval
				now := time.Now().Format(time.RFC3339)
				finalBackupLocation := path.Join(os.TempDir(), fmt.Sprintf("managed_tokens_vt_bak-%s-%s", serviceName, now))
				funcLogger.Errorf("Could not move previous token back to condor vault location.  Attempting to save it to %s", finalBackupLocation)
				if err := os.Rename(previousTokenTempFile.Name(), finalBackupLocation); err != nil {
					funcLogger.Errorf("Could not restore previously-existing vault token.  Will not delete backup copy made at %s", previousTokenTempFile.Name())
				}
				return errors.New("could not restore previous token")
			}
			funcLogger.Debugf("Restored prior condor vault token from %s to %s", previousTokenTempFile.Name(), condorVaultTokenLocation)
			return nil
		}
		return restorePriorTokenFunc, nil
	}
	return
}

// stageStoredTokenFile checks to see if there already exists a vault token for the given service and
// credd.  If so, it will move that file to where HTCondor expects it (as defined by the return value of
// getCondorVaultLocation)
func stageStoredTokenFile(tokenRootPath, serviceName, credd string) error {
	funcLogger := log.WithFields(log.Fields{
		"service": serviceName,
		"credd":   credd,
	})
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)

	storedServiceCreddTokenLocation := getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd)
	if _, err := os.Stat(storedServiceCreddTokenLocation); errors.Is(err, os.ErrNotExist) {
		funcLogger.Infof("No service credd token exists at %s.", storedServiceCreddTokenLocation)
		return errNoServiceCreddToken
	}

	if err := os.Rename(storedServiceCreddTokenLocation, condorVaultTokenLocation); err != nil {
		funcLogger.Error("Could not move stored service-credd vault token into place.  Will attempt to remove file at condor vault token location to ensure that a fresh one is generated.")
		if err2 := os.Remove(condorVaultTokenLocation); err2 != nil {
			funcLogger.Error("Could not remove condor vault token after failure to move stored service-credd vault token into place.  Please investigate")
			return err2
		}
		return errMoveServiceCreddToken
	}

	funcLogger.WithFields(log.Fields{
		"storageLocation":          storedServiceCreddTokenLocation,
		"condorVaultTokenLocation": condorVaultTokenLocation,
	}).Info("Successfully moved stored token into place for storage in vault and credd")
	return nil
}

// storeServiceTokenForCreddFile moves the vault token in the condor staging path (defined by getCondorVaultLocation)
// to the service-credd storage path (defined by getServiceTokenForCreddLocation)
func storeServiceTokenForCreddFile(tokenRootPath, serviceName, credd string) error {
	funcLogger := log.WithFields(log.Fields{
		"service": serviceName,
		"credd":   credd,
	})
	condorVaultTokenLocation := getCondorVaultTokenLocation(serviceName)
	storedServiceCreddTokenLocation := getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd)

	funcLogger.Debug("Attempting to move condor vault token to service-credd vault token storage path")
	err := os.Rename(condorVaultTokenLocation, storedServiceCreddTokenLocation)
	if err != nil {
		funcLogger.Errorf("Could not move condor vault token to service-credd vault storage path: %s", err)
		return err
	}
	funcLogger.Infof("Successfully moved condor vault token to service-credd vault storage path: %s", storedServiceCreddTokenLocation)
	return nil
}

// getCondorVaultTokenLocation returns the location of vault token that HTCondor uses based on the current user's UID
func getCondorVaultTokenLocation(serviceName string) string {
	var uid string
	currentUser, err := user.Current()
	if err != nil {
		log.WithField("service", serviceName).Error(`Could not get current user.  Will use string "000" instead`)
		uid = "000"
	} else {
		uid = currentUser.Uid
	}
	filename := fmt.Sprintf("vt_u%s-%s", uid, serviceName)
	return path.Join(os.TempDir(), filename)
}

func getServiceTokenForCreddLocation(tokenRootPath, serviceName, credd string) string {
	funcLogger := log.WithFields(log.Fields{
		"tokenRootPath": tokenRootPath,
		"service":       serviceName,
		"credd":         credd,
	})
	var uid string
	currentUser, err := user.Current()
	if err != nil {
		funcLogger.Error(`Could not get current user.  Will use string "000" instead`)
		uid = "000"
	} else {
		uid = currentUser.Uid
	}

	tokenFilename := fmt.Sprintf("vt_u%s-%s-%s", uid, credd, serviceName)
	return path.Join(tokenRootPath, tokenFilename)
}

var (
	errNoServiceCreddToken   = errors.New("no prior service credd token exists")
	errMoveServiceCreddToken = errors.New("could not move service credd token into place")
)
