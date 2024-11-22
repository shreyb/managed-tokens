// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package worker

import (
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
)

// NewChannelsForWorkers returns a ChannelsForWorkers that is initialized and ready to pass to workers and listen on
func NewChannelsForWorkers(bufferSize int) channelGroup {
	var useBufferSize int
	if bufferSize == 0 {
		useBufferSize = 1
	} else {
		useBufferSize = bufferSize
	}

	return channelGroup{
		serviceConfigChan: make(chan *Config, useBufferSize),
		successChan:       make(chan SuccessReporter, useBufferSize),
		notificationsChan: make(chan notifications.Notification, useBufferSize),
	}
}

// SuccessReporter is an interface to objects that report success or failures from various workers
type SuccessReporter interface {
	GetService() service.Service
	GetSuccess() bool
}

// channelGroup bundles the channels needed for workers to receive work, report whether that work succeeded or failed, and send notifications for routing
type channelGroup struct {
	serviceConfigChan chan *Config
	successChan       chan SuccessReporter
	notificationsChan chan notifications.Notification
}

func (c channelGroup) GetServiceConfigChan() chan *Config {
	return c.serviceConfigChan
}

func (c channelGroup) GetSuccessChan() chan SuccessReporter {
	return c.successChan
}

func (c channelGroup) GetNotificationsChan() chan notifications.Notification {
	return c.notificationsChan
}
