package worker

import (
	"fmt"
	"os/user"

	log "github.com/sirupsen/logrus"
)

func PushTokensWorker(inputChan <-chan *ServiceConfig, doneChan chan<- struct{}) {
	defer close(doneChan)
	for sc := range inputChan {
		service := sc.Experiment + "_" + sc.Role

		// kswitch
		if err := switchKerberosCache(sc); err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Fatal("Could not switch kerberos caches")
		}

		// TODO Hopefully we won't need this bit with the current UID if I can get htgettoken to write out vault tokens to a random tempfile
		// TODO Delete the source file.  Like with a defer os.Remove or something like that
		currentUser, err := user.Current()
		if err != nil {
			log.WithFields(log.Fields{
				"experiment": sc.Experiment,
				"role":       sc.Role,
			}).Fatal(err)
		}
		currentUID := currentUser.Uid

		sourceFilename := fmt.Sprintf("/tmp/vt_u%s-%s", currentUID, service)
		destinationFilenames := []string{
			fmt.Sprintf("/tmp/vt_u%d", sc.DesiredUID),
			fmt.Sprintf("/tmp/vt_u%d-%s", sc.DesiredUID, service),
		}

		// Send to nodes
		numFailures := 0
		for _, destinationNode := range sc.Nodes {
			for _, destinationFilename := range destinationFilenames {
				if err := pushToNodes(sc, sourceFilename, destinationNode, destinationFilename); err != nil {
					log.WithFields(log.Fields{
						"experiment": sc.Experiment,
						"role":       sc.Role,
					}).Error("Error pushing tokens to destination nodes")
					numFailures += 1
				}
			}
		}

		if numFailures == 0 {
			sc.Success = true
		}
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
			"experiment":          sc.Experiment,
			"role":                sc.Role,
			"sourceFilename":      sourceFile,
			"destinationFilename": destinationFile,
		}).Errorf("Could not copy file to destination")
		return err
	}
	log.WithFields(log.Fields{
		"experiment":          sc.Experiment,
		"role":                sc.Role,
		"sourceFilename":      sourceFile,
		"destinationFilename": destinationFile,
	}).Info("Success copying file to destination")
	return nil

}
