package notifications

import (
	"context"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/db"
)

// EmailManager is simply a channel on which Notification objects can be sent and received
type ServiceEmailManager struct {
	ReceiveChan         chan Notification
	Service             string
	Email               *email
	Database            *db.ManagedTokensDatabase
	NotificationMinimum int
}

type ServiceEmailManagerOption func(*ServiceEmailManager) error

// NewServiceEmailManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification, and when EmailManager is closed, will send emails.  Set up the ManagedTokensDatabase and
// the NotificationMinimum via EmailManagerOptions passed in.  If a ManagedTokensDatabase is not passed in via an EmailManagerOption,
// then the EmailManager will send all notifications
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, service string, e *email, opts ...ServiceEmailManagerOption) *ServiceEmailManager {
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.NewServiceEmailManager",
		"service": service,
	})

	em := &ServiceEmailManager{
		Service:     service,
		ReceiveChan: make(chan Notification),
	}
	for _, opt := range opts {
		if err := opt(em); err != nil {
			funcLogger.Errorf("Error running functional option")
		}
	}

	// Get our previous error information for this service
	ec, trackErrorCounts := setErrorCountsByService(ctx, em.Service, em.Database)

	// Set up the various admin channels needed
	adminChan := make(chan Notification)
	startAdminErrorAdder(adminChan)

	// Start listening for new notifications
	go func() {
		serviceErrorsTable := make(map[string]string, 0)
		defer wg.Done()
		defer close(adminChan)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					funcLogger.Error("Timeout exceeded in notification Manager")

				} else {
					funcLogger.Error(err)
				}
				return

			case n, chanOpen := <-em.ReceiveChan:
				// Channel is closed --> save errors to database and send notifications
				if !chanOpen {
					if trackErrorCounts {
						if err := saveErrorCountsInDatabase(ctx, em.Service, em.Database, ec); err != nil {
							funcLogger.Error("Error saving new error counts in database.  Please investigate")
						}
					}
					if len(serviceErrorsTable) > 0 {
						tableString := aggregateServicePushErrors(serviceErrorsTable)
						msg, err := prepareServiceEmail(ctx, tableString, e)
						if err != nil {
							funcLogger.Error("Error preparing service email for sending")
						}
						if err = SendMessage(ctx, e, msg); err != nil {
							funcLogger.Error("Error sending email")
						}
					}
					return
				}
				// Channel is open: direct the message as needed
				log.WithFields(log.Fields{
					"service": n.GetService(),
					"message": n.GetMessage(),
				}).Debug("Received notification message")
				shouldSend := true
				if trackErrorCounts {
					shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, ec, em.NotificationMinimum)
					if !shouldSend {
						log.WithField("service", n.GetService()).Debug("Error count less than error limit.  Not sending notification")
					}
				}
				if shouldSend {
					msg := "Error counts either not tracked or exceeded error limit.  Sending notification"
					if nValue, ok := n.(*pushError); ok {
						serviceErrorsTable[nValue.node] = n.GetMessage()
						log.WithFields(log.Fields{
							"service": nValue.service,
							"node":    nValue.node,
						}).Debug(msg)
					} else {
						log.WithField("service", n.GetService()).Debug(msg)
					}
					adminChan <- n
				}
			}
		}
	}()
	return em
}

// prepareServiceEmail sets a passed-in email object's templateStruct field to the passed in errorTable, and returns a string that contains
// email text according to the passed in errorTable and the email object's templatePath
func prepareServiceEmail(ctx context.Context, errorTable string, e *email) (string, error) {
	timestamp := time.Now().Format(time.RFC822)
	templateStruct := struct {
		Timestamp  string
		ErrorTable string
	}{
		Timestamp:  timestamp,
		ErrorTable: errorTable,
	}
	return prepareMessageFromTemplate(strings.NewReader(serviceErrorsTemplate), templateStruct)
}
