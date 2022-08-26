package worker

import (
	"context"

	"github.com/shreyb/managed-tokens/kerberos"
	"github.com/shreyb/managed-tokens/notifications"
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

const kerberosDefaultTimeoutStr string = "20s"

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
	defer close(chans.GetSuccessChan())
	defer close(chans.GetNotificationsChan())

	kerberosTimeout, err := utils.GetProperTimeoutFromContext(ctx, kerberosDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse kerberos timeout")
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

			if err := kerberos.GetTicket(kerbContext, sc.KeytabPath, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
				msg := "Could not obtain kerberos ticket"
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error(msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.Service.Name())
				return
			}

			if err := kerberos.CheckPrincipal(kerbContext, sc.UserPrincipal, sc.CommandEnvironment); err != nil {
				msg := "Kerberos ticket verification failed"
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error(msg)
				chans.GetNotificationsChan() <- notifications.NewSetupError(msg, sc.Service.Name())
			} else {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Debug("Kerberos ticket obtained and verified")
				success.success = true
			}
		}()
	}
}
