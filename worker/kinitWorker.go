package worker

import (
	"context"
	"time"

	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

const kerberosTimeoutStr string = "20s"

type kinitSuccess struct {
	serviceName string
	success     bool
}

func (v *kinitSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *kinitSuccess) GetSuccess() bool {
	return v.success
}

func GetKerberosTicketsWorker(ctx context.Context, chans ChannelsForWorkers) {
	var kerberosTimeout time.Duration
	var ok bool
	var err error
	defer close(chans.GetSuccessChan())

	if kerberosTimeout, ok = utils.GetOverrideTimeoutFromContext(ctx); !ok {
		log.WithField("func", "GetKerberosTicketsWorker").Debug("No overrideTimeout set.  Will use default")
		kerberosTimeout, err = time.ParseDuration(kerberosTimeoutStr)
		if err != nil {
			log.Fatal("Could not parse kerberos timeout duration")
		}
	}

	for sc := range chans.GetServiceConfigChan() {
		success := &kinitSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(k *kinitSuccess) {
				chans.GetSuccessChan() <- k
			}(success)

			kerbContext, kerbCancel := context.WithTimeout(ctx, kerberosTimeout)
			defer kerbCancel()

			if err := utils.GetKerberosTicket(kerbContext, sc); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Could not obtain kerberos ticket")
				return
			}

			if err := utils.CheckKerberosPrincipal(kerbContext, sc); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Kerberos ticket verification failed")
			} else {
				// TODO Make this debug
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Info("Kerberos ticket obtained and verified")
				success.success = true
			}
		}()
	}
}
