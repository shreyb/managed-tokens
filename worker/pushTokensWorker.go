package worker

import (
	"fmt"
	"os/user"

	log "github.com/sirupsen/logrus"
)

func PushTokensWorker(inputChan <-chan *ServiceConfig, successChan chan<- SuccessReporter) {
	defer close(successChan)
	for sc := range inputChan {
		pushSuccess := &pushTokenSuccess{
			serviceName: sc.Service.Name(),
		}

		func() {
			defer func(p *pushTokenSuccess) {
				successChan <- p
			}(pushSuccess)

			// kswitch
			if err := switchKerberosCache(sc); err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error("Could not switch kerberos caches")
				return
			}

			// TODO Hopefully we won't need this bit with the current UID if I can get htgettoken to write out vault tokens to a random tempfile
			// TODO Delete the source file.  Like with a defer os.Remove or something like that
			currentUser, err := user.Current()
			if err != nil {
				log.WithFields(log.Fields{
					"experiment": sc.Service.Experiment(),
					"role":       sc.Service.Role(),
				}).Error(err)
				return
			}
			currentUID := currentUser.Uid

			sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, sc.Service.Name())
			destinationFilenames := []string{
				fmt.Sprintf("/tmp/vt_u%d", sc.DesiredUID),
				fmt.Sprintf("/tmp/vt_u%d-%s", sc.DesiredUID, sc.Service.Name()),
			}

			// Send to nodes
			numFailures := 0
			for _, destinationNode := range sc.Nodes {
				for _, destinationFilename := range destinationFilenames {
					if err := pushToNodes(sc, sourceFilename, destinationNode, destinationFilename); err != nil {
						log.WithFields(log.Fields{
							"experiment": sc.Service.Experiment(),
							"role":       sc.Service.Role(),
						}).Error("Error pushing tokens to destination nodes")
						numFailures += 1
					}
				}
			}

			if numFailures == 0 {
				pushSuccess.success = true
			}
		}()

	}
}

func pushToNodes(sc *ServiceConfig, sourceFile, node, destinationFile string) error {
	rsyncConfig := NewRsyncSetup(
		sc.Account,
		node,
		destinationFile,
		"",
		&sc.CommandEnvironment,
	)

	if err := rsyncConfig.CopyToDestination(sourceFile); err != nil {
		log.WithFields(log.Fields{
			"experiment":          sc.Service.Experiment(),
			"role":                sc.Service.Role(),
			"sourceFilename":      sourceFile,
			"destinationFilename": destinationFile,
			"node":                node,
		}).Errorf("Could not copy file to destination")
		return err
	}
	log.WithFields(log.Fields{
		"experiment":          sc.Service.Experiment(),
		"role":                sc.Service.Role(),
		"sourceFilename":      sourceFile,
		"destinationFilename": destinationFile,
		"node":                node,
	}).Info("Success copying file to destination")
	return nil

}

type pushTokenSuccess struct {
	serviceName string
	success     bool
}

func (v *pushTokenSuccess) GetServiceName() string {
	return v.serviceName
}

func (v *pushTokenSuccess) GetSuccess() bool {
	return v.success
}
