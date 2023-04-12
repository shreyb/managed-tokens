package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/internal/notifications"
	"github.com/shreyb/managed-tokens/internal/service"
)

var serviceNotificationChanMap sync.Map                                  // Map of service to its notifications channel
var notificationsFromWorkersChan = make(chan notifications.Notification) // Global notifications chan that aggregates all notifications from workers
var workerNotificationWg sync.WaitGroup                                  // Waitgroup for the workers sending notifications
var notificationsManagersWg sync.WaitGroup                               // WaitGroup for the notifications.{Service|Admin}EmailManagers to finish their operations
var notificationSorter sync.Once                                         // sync.Once to make sure that we only start directNotificationstoManager once

// registerServiceNotificationsChan starts up a new notifications.EmailManager for a service and registers that EmailManager to the service name.
// This registration is stored in the serviceNotificationsChanMap.  It also increments a waitgroup so the caller can keep track of how many
// EmailManagers have been opened.
func registerServiceNotificationsChan(ctx context.Context, s service.Service, wg *sync.WaitGroup) {
	var serviceName string
	if val, ok := s.(*ExperimentOverriddenService); ok {
		serviceName = val.ConfigName()
	} else {
		serviceName = s.Name()
	}
	timestamp := time.Now().Format(time.RFC822)
	e := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice("experiments."+s.Experiment()+".emails"),
		fmt.Sprintf("Managed Tokens Push Errors for %s - %s", serviceName, timestamp),
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
		viper.GetString("templates.serviceerrors"),
	)
	wg.Add(1)
	m := notifications.NewServiceEmailManager(ctx, wg, serviceName, e)
	serviceNotificationChanMap.Store(serviceName, m)
}

// startListenerOnWorkerNotificationChans starts up directNotificationsToManagrs exactly once.  It then takes the
// notifications.Notification chan (nChan) and listens on it for worker notifications, sending notifications along to
// directNotificationsToManagers.  This func is kept separate from directNotificationsToManagers so that each caller that
// instantiates a notifications.Notifications chan can register its own listener (this func), and all those notifications are
// aggregated elsewhere
func startListenerOnWorkerNotificationChans(ctx context.Context, nChan chan notifications.Notification) {
	f := func() { directNotificationsToManagers(ctx) } // TODO (move to next line?)
	go func() {
		notificationSorter.Do(f)
	}()
	workerNotificationWg.Add(1)
	go func() {
		defer workerNotificationWg.Done()
		for n := range nChan {
			select {
			case <-ctx.Done():
				return
			default:
				notificationsFromWorkersChan <- n
			}
		}
	}()
}

// directNotificationsToManagers sorts notifications from notificationsFromWorkersChan and sends them to the appropriate registered
// notifications.EmailManager.  It is meant to be called only once.
func directNotificationsToManagers(ctx context.Context) {
	defer func() {
		var once sync.Once
		once.Do(closeRegisteredNotificationsChans)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case n, chanOpen := <-notificationsFromWorkersChan:
			if !chanOpen {
				return
			}
			if ch, ok := serviceNotificationChanMap.Load(n.GetService()); ok {
				if chVal, ok := ch.(notifications.EmailManager); ok {
					chVal <- n
				} else {
					log.Errorf("Registered service notification channel is of wrong type %T.  Expected chan notification.Notification", chVal)
				}
			} else {
				log.Errorf("No notification channel exists for service %s", n.GetService())
			}
		}
	}
}

// closeRegisteredNotificationsChans closes all the channels registered in serviceNotificationChanMap
func closeRegisteredNotificationsChans() {
	serviceNotificationChanMap.Range(
		func(key, value any) bool {
			if ch, ok := value.(notifications.EmailManager); ok {
				close(ch)
			} else {
				log.Errorf("Registered service notification channel is of wrong type %T.  Expected chan notifications.Notification", ch)
			}

			return true
		},
	)
	log.WithField("executable", currentExecutable).Debug("Closed all notifications channels")
}

// handleNotificationsFinalization Handle cleanup and sending of notifications when we're all done
func handleNotificationsFinalization() {
	workerNotificationWg.Wait()         // First let all workers finish sending notifications through their various Notifications channels
	close(notificationsFromWorkersChan) // Close the aggregation channel for all worker notifications
	notificationsManagersWg.Wait()      // Wait for all notifications.{Service|Admin}EmailManager instances to finish their work
}
