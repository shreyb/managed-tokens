package worker

import (
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func init() {
	// Get Kerberos templates into the kerberosExecutables map
	// TODO Make this use utils.CheckforExecutables
	for kExe := range kerberosExecutables {
		kPath, err := exec.LookPath(kExe)
		if err != nil {
			log.Errorf("Could not find executable %s.  Please ensure it exists on your system", kExe)
			os.Exit(1)
		}
		kerberosExecutables[kExe] = kPath
		log.Infof("Using %s executable: %s", kExe, kPath)
	}

}

func GetKerberosTicketsWorker(inputChan <-chan *ServiceConfig, doneChan chan<- struct{}) {
	defer close(doneChan)
	for sc := range inputChan {
		if err := getKerberosTicket(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Fatal("Could not obtain kerberos ticket")
		}

		if err := checkKerberosPrincipal(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Fatal("Kerberos ticket verification failed")
		}
	}
}
