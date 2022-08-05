package worker

import (
	"context"
	"strings"
	"sync"

	"github.com/shreyb/managed-tokens/service"
	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

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

func PingAggregatorWorker(chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	var wg sync.WaitGroup
	defer wg.Wait()

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
			nodes := make([]utils.PingNoder, 0, len(sc.Nodes))
			for _, node := range sc.Nodes {
				nodes = append(nodes, utils.NewNode(node))
			}

			//TODO Change this context
			ctx := context.Background()
			pingStatus := utils.PingAllNodes(ctx, nodes...)

			failedNodes := make([]utils.PingNoder, 0, len(sc.Nodes))
			for status := range pingStatus {
				if status.Err != nil {
					log.WithFields(log.Fields{
						"service": sc.Service.Name(),
						"node":    status.PingNoder.String(),
					}).Error("Error pinging node")
					failedNodes = append(failedNodes, status.PingNoder)
				} else {
					// TODO: Make debug
					log.WithFields(log.Fields{
						"service": sc.Service.Name(),
						"node":    status.PingNoder.String(),
					}).Info("Successfully pinged node")
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
			}
		}(sc)
	}
}
