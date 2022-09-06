package worker

import (
	"github.com/shreyb/managed-tokens/internal/notifications"
)

// ChannelsForWorkers provides an interface to types that bundle a chan *Config, chan SuccessReporter, and chan notifications.Notification
// This is meant to be the primary value passed around between functions and packages to orchestrate communication
type ChannelsForWorkers interface {
	GetServiceConfigChan() chan *Config
	GetSuccessChan() chan SuccessReporter
	GetNotificationsChan() chan notifications.Notification
}

// NewChannelsForWorkers returns a ChannelsForWorkers that is initialized and ready to pass to workers and listen on
func NewChannelsForWorkers(bufferSize int) ChannelsForWorkers {
	var useBufferSize int
	if bufferSize == 0 {
		useBufferSize = 1
	} else {
		useBufferSize = bufferSize
	}

	return &channelGroup{
		serviceConfigChan: make(chan *Config, useBufferSize),
		successChan:       make(chan SuccessReporter, useBufferSize),
		notificationsChan: make(chan notifications.Notification, useBufferSize),
	}
}

// SuccessReporter is an interface to objects that report success or failures from various workers
type SuccessReporter interface {
	GetServiceName() string
	GetSuccess() bool
}

// channelGroup bundles the channels needed for workers to receive work, report whether that work succeeded or failed, and send notifications for routing
type channelGroup struct {
	serviceConfigChan chan *Config
	successChan       chan SuccessReporter
	notificationsChan chan notifications.Notification
}

func (c *channelGroup) GetServiceConfigChan() chan *Config {
	return c.serviceConfigChan
}

func (c *channelGroup) GetSuccessChan() chan SuccessReporter {
	return c.successChan
}

func (c *channelGroup) GetNotificationsChan() chan notifications.Notification {
	return c.notificationsChan
}
