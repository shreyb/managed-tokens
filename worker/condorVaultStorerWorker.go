package worker

import (
	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
)

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

			if err := utils.StoreAndGetTokens(sc, interactive); err != nil {
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
	return utils.StoreAndGetTokens(sc, interactive)
}
