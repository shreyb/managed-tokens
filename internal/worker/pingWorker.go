package worker

import (
	"context"
	"strings"
	"sync"

	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/ping"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
)

const pingDefaultTimeoutStr string = "10s"

type pingSuccess struct {
	serviceName string
	success     bool
}

func (p *pingSuccess) GetServiceName() string {
	return p.serviceName
}

func (p *pingSuccess) GetSuccess() bool {
	return p.success
}

func PingAggregatorWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer close(chans.GetNotificationsChan())
	var wg sync.WaitGroup
	defer wg.Wait()

	pingTimeout, err := utils.GetProperTimeoutFromContext(ctx, pingDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse ping timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		wg.Add(1)
		go func(sc *service.Config) {
			defer wg.Done()
			success := &pingSuccess{
				serviceName: sc.Service.Name(),
			}

			defer func(p *pingSuccess) {
				chans.GetSuccessChan() <- p
			}(success)

			// Prepare my slice of PingNoders
			nodes := make([]ping.PingNoder, 0, len(sc.Nodes))
			for _, node := range sc.Nodes {
				nodes = append(nodes, ping.NewNode(node))
			}

			pingContext, pingCancel := context.WithTimeout(ctx, pingTimeout)
			defer pingCancel()
			pingStatus := ping.PingAllNodes(pingContext, nodes...)

			failedNodes := make([]ping.PingNoder, 0, len(sc.Nodes))
			for status := range pingStatus {
				if status.Err != nil {
					log.WithFields(log.Fields{
						"service": sc.Service.Name(),
						"node":    status.PingNoder.String(),
					}).Error("Error pinging node")
					failedNodes = append(failedNodes, status.PingNoder)
				} else {
					log.WithFields(log.Fields{
						"service": sc.Service.Name(),
						"node":    status.PingNoder.String(),
					}).Debug("Successfully pinged node")
				}
			}
			if len(failedNodes) == 0 {
				success.success = true
				log.WithField("service", sc.Service.Name()).Info("Successfully pinged all nodes for service")
			} else {
				failedNodesStrings := make([]string, 0, len(failedNodes))
				for _, node := range failedNodes {
					failedNodesStrings = append(failedNodesStrings, node.String())
				}
				log.WithField("service", sc.Service.Name()).Error("Error pinging some of nodes for service")
				log.WithField("service", sc.Service.Name()).Errorf("Failed Nodes: %s", strings.Join(failedNodesStrings, ", "))
				chans.GetNotificationsChan() <- notifications.NewSetupError(
					"Could not ping the following nodes: "+strings.Join(failedNodesStrings, ", "),
					sc.Service.Name(),
				)
			}
		}(sc)
	}
}
