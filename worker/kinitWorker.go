package worker

import (
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	if err := utils.CheckForExecutables(kerberosExecutables); err != nil {
		log.Fatal("Could not find kerberos executables")
	}
}

func GetKerberosTicketsWorker(chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	for sc := range chans.GetServiceConfigChan() {
		success := &kinitSuccess{
			serviceName: sc.Service.Name(),
		}
		if err := getKerberosTicket(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Error("Could not obtain kerberos ticket")
		}

		if err := checkKerberosPrincipal(sc); err != nil {
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
		chans.GetSuccessChan() <- success
	}
}

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
