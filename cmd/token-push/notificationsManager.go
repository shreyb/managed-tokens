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

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/fermitools/managed-tokens/internal/cmdUtils"
	"github.com/fermitools/managed-tokens/internal/notifications"
	"github.com/fermitools/managed-tokens/internal/service"
)

// General outline of how this works:
// 1.  registerServiceNotificationsChan is called with each service config setup.  This starts a notification.ServiceEmailManager, tracked by
// serviceEmailManagersWg.  It also registers the service name and corresponding ServiceEmailManager's notifications channel in the
// serviceNotificationChanMap
// 2.  When a worker starts up, it will register itself by calling startListenerOnWorkerNotificationChans.  The first time this is called,
// it will start up a  directNotificationsToManagers.  Each time, the workerNotificationWg is incremented
// 3.  As a notification comes in, the startListenerOnWorkerNotificationsChans listener will forward it on the notificationsFromWorkersChan to
// the directNotificationsToManagers aggregator.  The latter will check the serviceNotificationChanMap to figure out which
// notifications.ServiceEmailManager's notifications channel to forward the message to.
//
// When all operations are done, handleNotificationsFinalization should be called.  This function:
// 1.  Waits for all the workers to finish sending notifications to their listeners (controlled by workerNotificationWg)
// 2.  Closes the notificationsFromWorkersChan to signal the internal routing functions to expect no more notifications to come through
// 3.  (2) closes all the registered ServiceEmailManager notifications channels in the serviceNotificationChanMap.  This triggers those
// ServiceEmailManagers to compile their data and send emails to service stakeholders if needed
// 4.  Waits for all registered ServiceEmailManagers to finish their work (controlled by serviceEmailManagersWg)

var serviceNotificationChanMap sync.Map                                  // Map of service to its notifications channel
var serviceEmailManagersWg sync.WaitGroup                                // WaitGroup to keep track of active ServiceEmailManagers for the configured services
var notificationSorterOnce sync.Once                                     // sync.Once to make sure that we only start directNotificationstoManager once
var workerNotificationWg sync.WaitGroup                                  // Waitgroup for the workers sending notifications.  Incremented and decremented every time startListenerOnWorkerNotificationChans is called/returned from
var notificationsFromWorkersChan = make(chan notifications.Notification) // Global notifications chan that aggregates all notifications from workers

// External functions meant to be called to register, route, and clean up notifications management

// registerServiceNotificationsChan starts up a new notifications.ServiceEmailManager for a service and registers that ServiceEmailManager's
// notifications channel to the service name.  This registration is stored in the serviceNotificationChanMap.  It also increments a waitgroup
// so the caller can keep track of how many ServiceEmailManagers have been opened.
func registerServiceNotificationsChan(ctx context.Context, s service.Service, a *notifications.AdminNotificationManager) {
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "registerServiceNotificationsChan")
	defer span.End()

	serviceName := cmdUtils.GetServiceName(s)
	span.SetAttributes(attribute.KeyValue{Key: "service", Value: attribute.StringValue(serviceName)})

	toEmails := viper.GetStringSlice("experiments." + s.Experiment() + ".emails")
	if len(toEmails) == 0 {
		exeLogger.WithField("service", serviceName).Warn("No emails found.  No notifications will be sent for this service")
		return
	}

	timestamp := time.Now().Format(time.RFC822)
	e := notifications.NewEmail(
		viper.GetString("email.from"),
		toEmails,
		fmt.Sprintf("Managed Tokens Push Errors for %s - %s", serviceName, timestamp),
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
	)
	serviceEmailManagersWg.Add(1)

	// Functional options for ServiceEmailManager
	funcOpts := make([]notifications.ServiceEmailManagerOption, 0)
	setNotificationMinimum := func(em *notifications.ServiceEmailManager) error {
		em.NotificationMinimum = viper.GetInt("errorCountToSendMessage")
		return nil
	}
	funcOpts = append(funcOpts, setNotificationMinimum)

	if a != nil {
		setAdminNotificationManager := func(em *notifications.ServiceEmailManager) error {
			em.AdminNotificationManager = a
			return nil
		}
		funcOpts = append(funcOpts, setAdminNotificationManager)
	}

	m := notifications.NewServiceEmailManager(ctx, &serviceEmailManagersWg, serviceName, e, funcOpts...) // Start our ServiceEmailManager
	serviceNotificationChanMap.Store(serviceName, m.ReceiveChan)                                         // Register the ServiceEmailManager's notification chan so we can route notifications later
}

// startListenerOnWorkerNotificationChans starts up directNotificationsToManagrs exactly once.  It then listens on the
// notifications.Notification chan (nChan) for worker notifications, sending notifications along to directNotificationsToManagers.
// This func is meant for external callers to register their own notifications channel so that any passed in notifications can
// be routed appropriately.
func startListenerOnWorkerNotificationChans(ctx context.Context, nChan chan notifications.Notification) {
	ctx, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "startListenerOnWorkerNotificationChans")
	defer span.End()

	// Start up the aggregator exactly once
	go func() {
		f := func() { directNotificationsToManagers(ctx) }
		notificationSorterOnce.Do(f)
	}()

	// Start up a listener for the caller, directing its notifications to notificationsFromWorkerChan
	workerNotificationWg.Add(1)
	go func() {
		defer workerNotificationWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case n, chanOpen := <-nChan:
				if !chanOpen {
					return
				}
				notificationsFromWorkersChan <- n
			}
		}
	}()
}

// handleNotificationsFinalization Handle cleanup and sending of notifications when all operations are finished
func handleNotificationsFinalization() {
	workerNotificationWg.Wait()         // First let all workers finish sending notifications through their various Notifications channels
	close(notificationsFromWorkersChan) // Close the aggregation channel for all worker notifications
	serviceEmailManagersWg.Wait()       // Wait for all notifications.ServiceEmailManager instances to finish their work
}

// Internal routing funcs

// directNotificationsToManagers is the aggregator func that sorts notifications from notificationsFromWorkersChan and sends them to the
// appropriate registered notifications.ServiceEmailManager.  It is meant to be called only once.
func directNotificationsToManagers(ctx context.Context) {
	_, span := otel.GetTracerProvider().Tracer("token-push").Start(ctx, "directNotificationsToManagers")
	defer span.End()

	defer func() {
		var once sync.Once
		once.Do(closeRegisteredNotificationsChans)
	}()

	for n := range notificationsFromWorkersChan {
		select {
		case <-ctx.Done():
			return
		default:
			// Direct received notifications to appropriate registered notifications channel, if it exists
			if receiveChan, ok := serviceNotificationChanMap.Load(n.GetService()); ok {
				if receiveChanVal, ok := receiveChan.(chan notifications.Notification); ok {
					receiveChanVal <- n
				} else {
					exeLogger.Errorf("Registered service notification channel is of wrong type %T.  Expected chan notification.Notification", receiveChanVal)
				}
			}
		}
	}
}

// closeRegisteredNotificationsChans closes all the channels registered in serviceNotificationChanMap
func closeRegisteredNotificationsChans() {
	serviceNotificationChanMap.Range(
		func(key, value any) bool {
			if receiveChanVal, ok := value.(chan notifications.Notification); ok {
				close(receiveChanVal)
			} else {
				exeLogger.Errorf("Registered service email manager is of wrong type %T.  Expected chan notifications.Notification", receiveChanVal)
			}
			return true
		},
	)
	exeLogger.Debug("Closed all notifications channels")
}
