package worker

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

var condorExecutables = map[string]string{
	"condor_vault_storer": "",
	"condor_store_cred":   "",
}

func init() {
	// Check for condor_store_cred executable
	if _, err := exec.LookPath("condor_store_cred"); err != nil {
		log.Warn("Could not find condor_store_cred.  Adding /usr/sbin to $PATH")
		os.Setenv("PATH", "/usr/sbin:$PATH")
	}

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
			}).Fatal("Could not switch kerberos caches")
		}

		// Get tokens and store them in vault
		if err := getTokensandStoreinVault(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Fatal("Could not obtain vault token")
		}
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
	// TODO verify token scopes
}
