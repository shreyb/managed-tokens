package worker

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"

	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

var condorExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
}

func init() {
	os.Setenv("PATH", "/usr/bin:/usr/sbin")
	if err := utils.CheckForExecutables(condorExecutables); err != nil {
		log.Fatal("Could not find path to condor executables")
	}
}

func StoreAndGetTokenWorker(inputChan <-chan *ServiceConfig, successChan chan<- SuccessReporter) {
	defer close(successChan)
	var interactive bool
	for sc := range inputChan {
		success := &vaultStorerSuccess{
			serviceName: sc.Service.Name(),
		}
		if err := storeAndGetTokens(sc, interactive); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Error("Could not store and get vault tokens")
		} else {
			success.success = true
		}
		successChan <- success
	}
}

func StoreAndGetRefreshAndVaultTokens(sc *ServiceConfig) error {
	interactive := true
	return storeAndGetTokens(sc, interactive)
}

// TODO Move the rest of the funcs into a condorVaultStorerUtils.go file.  Keep the type vaultStorerSuccess here though

func storeAndGetTokens(sc *ServiceConfig, interactive bool) error {
	// kswitch
	if err := switchKerberosCache(sc); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
		}).Error("Could not switch kerberos caches")
		return err
	}

	// Get token and store it in vault
	if err := getTokensandStoreinVault(sc, interactive); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
		}).Error("Could not obtain vault token")
		return err
	}

	// Validate vault token
	currentUser, err := user.Current()
	if err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
		}).Error(err)
		return err
	}
	currentUID := currentUser.Uid
	vaultTokenFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, sc.Service.Name())

	if err := validateVaultToken(vaultTokenFilename); err != nil {
		log.WithFields(log.Fields{
			"experiment": sc.Service.Experiment(),
			"role":       sc.Service.Role(),
		}).Error("Could not validate vault token")
		return err
	}

	// TODO Make this a debug
	log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
	}).Info("Validated vault token")
	return nil
}

func getTokensandStoreinVault(sc *ServiceConfig, interactive bool) error {
	// Store token in vault and get new vault token
	//TODO if verbose, add the -v flag here
	getTokensAndStoreInVaultCmd := exec.Command(condorExecutables["condor_vault_storer"], sc.Service.Name())
	getTokensAndStoreInVaultCmd = environmentWrappedCommand(getTokensAndStoreInVaultCmd, &sc.CommandEnvironment)

	log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
	}).Info("Storing and obtaining vault token")

	if interactive {
		// We need to capture stdout and stderr on the terminal so the user can authenticate
		getTokensAndStoreInVaultCmd.Stdout = os.Stdout
		getTokensAndStoreInVaultCmd.Stderr = os.Stderr

		if err := getTokensAndStoreInVaultCmd.Start(); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Errorf("Error starting condor_vault_storer command to store and obtain tokens; %s", err.Error())
		}
		if err := getTokensAndStoreInVaultCmd.Wait(); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err.Error())
			return err
		}
	} else {
		if stdoutStderr, err := getTokensAndStoreInVaultCmd.CombinedOutput(); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Errorf("Error running condor_vault_storer to store and obtain tokens; %s", err.Error())
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Errorf("%s", stdoutStderr)
			return err
		} else {
			log.Infof("%s", stdoutStderr)
		}
	}

	log.WithFields(log.Fields{
		"experiment": sc.Service.Experiment(),
		"role":       sc.Service.Role(),
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

type vaultStorerSuccess struct {
	serviceName string
	success     bool
}

func (v *vaultStorerSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *vaultStorerSuccess) GetSuccess() bool {
	return v.success
}
