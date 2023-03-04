package worker

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/ping"
	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/utils"
	log "github.com/sirupsen/logrus"
)

const pingDefaultTimeoutStr string = "10s"

// pingSuccess is a type that conveys whether PingAggregatorWorker successfully pings all the configured destination nodes for each service
type pingSuccess struct {
	service.Service
	success bool
}

func (p *pingSuccess) GetService() service.Service {
	return p.Service
}

func (p *pingSuccess) GetSuccess() bool {
	return p.success
}

// PingAggregatorWorker is a worker that listens on chans.GetServiceConfigChan(), and for the received worker.Config objects,
// concurrently pings all of the Config's destination nodes.  It returns when chans.GetServiceConfigChan() is closed,
// and it will in turn close the other chans in the passed in ChannelsForWorkers
func PingAggregatorWorker(ctx context.Context, chans ChannelsForWorkers) {
	defer close(chans.GetSuccessChan())
	defer func() {
		close(chans.GetNotificationsChan())
		log.Debug("Closed PingAggregatorWorker Notifications Chan")
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	pingTimeout, err := utils.GetProperTimeoutFromContext(ctx, pingDefaultTimeoutStr)
	if err != nil {
		log.Fatal("Could not parse ping timeout")
	}

	for sc := range chans.GetServiceConfigChan() {
		wg.Add(1)
		go func(sc *Config) {
			defer wg.Done()
			success := &pingSuccess{
				Service: sc.Service,
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
					var msg string
					if errors.Is(status.Err, context.DeadlineExceeded) {
						msg = "Timeout error"
					} else {
						msg = "Error pinging node"
					}
					log.WithFields(log.Fields{
						"service": sc.Service.Name(),
						"node":    status.PingNoder.String(),
					}).Error(msg)
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
