package worker

import (
	// "fmt"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"

	// "os/user"

	// "github.com/lestrrat-go/jwx/jwt"
	// "github.com/scitokens/scitokens-go"
	log "github.com/sirupsen/logrus"
)

var condorExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
}

func init() {
	// Check for condor_store_cred executable
	os.Setenv("PATH", "/usr/bin:/usr/sbin")

	// TODO Make this use utils.CheckforExecutables
	for cExe := range condorExecutables {
		cPath, err := exec.LookPath((cExe))
		if err != nil {
			log.WithField("executable", cExe).Fatal("Could not find path to condor executable")
		}
		condorExecutables[cExe] = cPath
	}
}

func StoreAndGetTokenWorker(inputChan <-chan *ServiceConfig, doneChan chan<- struct{}) {
	defer close(doneChan)
	for sc := range inputChan {

		// kswitch
		if err := switchKerberosCache(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error("Could not switch kerberos caches")
			return
		}

		// Get token and store it in vault
		if err := getTokensandStoreinVault(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error("Could not obtain vault token")
			return
		}

		// Validate vault token
		currentUser, err := user.Current()
		if err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error(err)
			return
		}
		currentUID := currentUser.Uid
		service := sc.Experiment + "_" + sc.Role
		vaultTokenFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, service)

		if err := validateVaultToken(vaultTokenFilename); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Error("Could not validate vault token")
			return
		}

		// TODO Make this a debug
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Info("Validated vault token")

	}
}

func getTokensandStoreinVault(sc *ServiceConfig) error {

	// Store token in vault and get new vault token
	//TODO if verbose, add the -v flag here
	service := sc.Experiment + "_" + sc.Role
	getTokensAndStoreInVaultCmd := exec.Command(condorExecutables["condor_vault_storer"], service)
	getTokensAndStoreInVaultCmd = environmentWrappedCommand(getTokensAndStoreInVaultCmd, &sc.CommandEnvironment)

	log.WithFields(log.Fields{
		"experiment": sc.Experiment,
		"role":       sc.Role,
	}).Info("Storing and obtaining vault token")
	// TODO Figure out if I want stdout or not
	// if err := getTokensAndStoreInVaultCmd.Run(); err != nil {
	if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Error("Error running condor_vault_storer to store and obtain tokens; %s", err.Error())
		log.WithFields(log.Fields{
			"experiment": sc.Experiment,
			"role":       sc.Role,
		}).Errorf("%s", stdoutStderr)
		return err
	} else {
		log.Infof("%s", stdoutStderr)
	}
	log.WithFields(log.Fields{
		"experiment": sc.Experiment,
		"role":       sc.Role,
	}).Info("Successfully obtained and stored vault token")

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
		return errors.New(errString)
	}

	return nil
}
