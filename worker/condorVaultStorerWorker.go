package worker

import (
	"os"

	"github.com/shreyb/managed-tokens/service"
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

func StoreAndGetTokenWorker(chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	var interactive bool
	for sc := range chans.GetServiceConfigChan() {
		success := &vaultStorerSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(v *vaultStorerSuccess) {
				chans.GetSuccessChan() <- v
			}(success)

			if err := storeAndGetTokens(sc, interactive); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Could not store and get vault tokens")
			} else {
				success.success = true
			}
		}()
	}
}

func StoreAndGetRefreshAndVaultTokens(sc *service.Config) error {
	interactive := true
	return storeAndGetTokens(sc, interactive)
}
