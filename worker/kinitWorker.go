package worker

import (
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	// TODO Make this use utils.CheckforExecutables
	if err := utils.CheckForExecutables(kerberosExecutables); err != nil {
		log.Fatal("Could not find kerberos executables")
	}
}

func GetKerberosTicketsWorker(inputChan <-chan *ServiceConfig, doneChan chan<- struct{}) {
	defer close(doneChan)
	for sc := range inputChan {
		if err := getKerberosTicket(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Fatal("Could not obtain kerberos ticket")
		}

		if err := checkKerberosPrincipal(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Service.Experiment(),
				"role":       sc.Service.Role(),
			}).Fatal("Kerberos ticket verification failed")
		}
	}
}
