package notifications

import (
	"context"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/db"
)

// ServiceEmailManager contains all the information needed to receive Notifications for services and ensure they get sent in the
// correct email
type ServiceEmailManager struct {
	ReceiveChan         chan Notification
	Service             string
	Email               *email
	Database            *db.ManagedTokensDatabase
	NotificationMinimum int
	wg                  *sync.WaitGroup
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
		wg:          wg,
	}
	for _, opt := range opts {
		if err := opt(em); err != nil {
			funcLogger.Errorf("Error running functional option")
		}
	}

	ec, shouldTrackErrorCounts := setErrorCountsByService(ctx, em.Service, em.Database) // Get our previous error information for this service

	adminChan := make(chan Notification)
	startAdminErrorAdder(adminChan)
	runServiceNotificationHandler(ctx, em, adminChan, ec, shouldTrackErrorCounts)

	return em
}

// runServiceNotificationHandler concurrently handles the routing and counting of errors that result from a Notification being sent
// on the ServiceEmailManager's ReceiveChan.
func runServiceNotificationHandler(ctx context.Context, em *ServiceEmailManager, adminChan chan Notification, ec *serviceErrorCounts, shouldTrackErrorCounts bool) {
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.runServiceNotificationHandler",
		"service": em.Service,
	})

	// Start listening for new notifications
	go func() {
		serviceErrorsTable := make(map[string]string, 0)
		defer em.wg.Done()
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
					if shouldTrackErrorCounts {
						if err := saveErrorCountsInDatabase(ctx, em.Service, em.Database, ec); err != nil {
							funcLogger.Error("Error saving new error counts in database.  Please investigate")
						}
					}
					sendServiceEmailIfErrors(ctx, serviceErrorsTable, em)
					return
				}

				// Channel is open: direct the message as needed
				funcLogger.WithField("message", n.GetMessage()).Debug("Received notification message")
				shouldSend := true
				if shouldTrackErrorCounts {
					shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, ec, em.NotificationMinimum)
					if !shouldSend {
						log.WithField("service", n.GetService()).Debug("Error count less than error limit.  Not sending notification")
						continue
					}
				}
				if shouldSend {
					addPushErrorNotificationToServiceErrorsTable(n, serviceErrorsTable)
					adminChan <- n
				}
			}
		}
	}()
}

func addPushErrorNotificationToServiceErrorsTable(n Notification, serviceErrorsTable map[string]string) {
	// Note that we ONLY send push errors to the stakeholders.  Only admins will get all Notifications.
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.addPushErrorNotificationToServiceErrorsTable",
		"service": n.GetService(),
	})

	msg := "Error counts either not tracked or exceeded error limit.  Sending notification"
	if nValue, ok := n.(*pushError); ok {
		serviceErrorsTable[nValue.node] = n.GetMessage()
		funcLogger.WithField("node", nValue.node).Debug(msg)
		return
	}
	funcLogger.Debug(msg)
}

func sendServiceEmailIfErrors(ctx context.Context, serviceErrorsTable map[string]string, em *ServiceEmailManager) {
	funcLogger := log.WithFields(log.Fields{
		"caller":  "notifications.sendServiceEmailIfErrors",
		"service": em.Service,
	})

	if len(serviceErrorsTable) == 0 {
		funcLogger.Debug("No errors to send for service")
		return
	}

	tableString := aggregateServicePushErrors(serviceErrorsTable)
	msg, err := prepareServiceEmail(ctx, tableString, em.Email)
	if err != nil {
		funcLogger.Error("Error preparing service email for sending")
	}
	if err = SendMessage(ctx, em.Email, msg); err != nil {
		funcLogger.Error("Error sending email")
	}
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
