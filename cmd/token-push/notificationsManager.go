package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/shreyb/managed-tokens/notifications"
	"github.com/shreyb/managed-tokens/service"
)

var serviceNotificationChanMap sync.Map
var notificationsFromWorkersChan = make(chan notifications.Notification) // Global notifications chan that aggregates all notifications from workers
var workerNotificationWg sync.WaitGroup                                  // Waitgroup for the workers sending notifications
var notificationsManagersWg sync.WaitGroup                               // WaitGroup for the notifications.{Service|Admin}EmailManagers to finish their operations

// Populate the serviceNotificationsChanMap so that we know where to send notifications
func registerServiceNotificationsChan(ctx context.Context, s service.Service, wg *sync.WaitGroup) {
	timestamp := time.Now().Format(time.RFC822)
	e := notifications.NewEmail(
		viper.GetString("email.from"),
		viper.GetStringSlice("experiments."+s.Experiment()+".emails"),
		fmt.Sprintf("Managed Tokens Push Errors for %s - %s", s.Name(), timestamp),
		viper.GetString("email.smtphost"),
		viper.GetInt("email.smtpport"),
		viper.GetString("templates.serviceerrors"),
	)
	wg.Add(1)
	m := notifications.NewServiceEmailManager(ctx, wg, s.Name(), e)
	serviceNotificationChanMap.Store(s.Name(), m)
}

// Start listener for worker notifications
func registerWorkerNotificationChans(nChan chan notifications.Notification) {
	workerNotificationWg.Add(1)
	go func() {
		defer workerNotificationWg.Done()
		for n := range nChan {
			notificationsFromWorkersChan <- n
		}
	}()
}

// Sort notifications from notificationsFromWorkersChan and send them to the appropriate registered notifications.EmailManager
func directNotificationsToManagers(ctx context.Context) {
	defer func() {
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

// Handle cleanup and sending of notifications when we're all done
func handleNotificationsFinalization() {
	workerNotificationWg.Wait()         // First let all workers finish sending notifications through their various Notifications channels
	close(notificationsFromWorkersChan) // Close the aggregation channel for all worker notifications
	notificationsManagersWg.Wait()      // Wait for all notifications.{Service|Admin}EmailManager instances to finish their work
}
