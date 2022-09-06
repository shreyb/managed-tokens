package worker

import (
	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
)

type ChannelsForWorkers interface {
	GetServiceConfigChan() chan *service.Config
	GetSuccessChan() chan SuccessReporter
	GetNotificationsChan() chan notifications.Notification
}

type SuccessReporter interface {
	GetServiceName() string
	GetSuccess() bool
}

func NewChannelsForWorkers(bufferSize int) ChannelsForWorkers {
	var useBufferSize int
	if bufferSize == 0 {
		useBufferSize = 1
	} else {
		useBufferSize = bufferSize
	}

	return &channelGroup{
		serviceConfigChan: make(chan *service.Config, useBufferSize),
		successChan:       make(chan SuccessReporter, useBufferSize),
		notificationsChan: make(chan notifications.Notification, useBufferSize),
	}
}

type channelGroup struct {
	serviceConfigChan chan *service.Config
	successChan       chan SuccessReporter
	notificationsChan chan notifications.Notification
}

func (c *channelGroup) GetServiceConfigChan() chan *service.Config {
	return c.serviceConfigChan
}

func (c *channelGroup) GetSuccessChan() chan SuccessReporter {
	return c.successChan
}

func (c *channelGroup) GetNotificationsChan() chan notifications.Notification {
	return c.notificationsChan
}
