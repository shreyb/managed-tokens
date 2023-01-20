package worker

import (
	"context"
	"errors"

	"github.com/shreyb/managed-tokens/internal/kerberos"
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
)

const kerberosDefaultTimeoutStr string = "20s"

// kinitSuccess is a type that conveys whether GetKerberosTicketsWorker successfully obtains a kerberos ticket for each service
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

// GetKerberosTicketsWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// obtains kerberos tickets from the configured kerberos principals.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func GetKerberosTicketsWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed Kerberos Tickets Worker Notifications Chan")
	}()

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
				var msg string
				if errors.Is(err, context.DeadlineExceeded) {
					msg = "Timeout error"
				} else {
					msg = "Could not obtain kerberos ticket"
				}
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
