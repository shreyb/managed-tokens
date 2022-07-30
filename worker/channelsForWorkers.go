package worker

// TODO Add notifications channel to this interface and accompanying type?
// TODO Have workers that use channels to communicate accept this interface rather than explicit channels

type ChannelsForWorkers interface {
	GetServiceConfigChan() chan *ServiceConfig
	GetSuccessChan() chan SuccessReporter
	// GetNotificationsChan() chan *notifications.notification or whatever it's actually called

}

func NewChannelsForWorkers(bufferSize int) ChannelsForWorkers {
	var useBufferSize int
	if bufferSize == 0 {
		useBufferSize = 1
	} else {
		useBufferSize = bufferSize
	}

	return &channelGroup{
		serviceConfigChan: make(chan *ServiceConfig, useBufferSize),
		successChan:       make(chan SuccessReporter, useBufferSize),
		// notificationsChan: make(chan *notifications.notification, useBufferSize),
	}
}

type channelGroup struct {
	serviceConfigChan chan *ServiceConfig
	successChan       chan SuccessReporter
	// notificationsChan chan *notifications.notification
}

func (c *channelGroup) GetServiceConfigChan() chan *ServiceConfig {
	return c.serviceConfigChan
}

func (c *channelGroup) GetSuccessChan() chan SuccessReporter {
	return c.successChan
}

// func (c *channelGroup) GetNotificationsChan() chan *notifications.notification{
// 	return c.notificationsChan
// }
