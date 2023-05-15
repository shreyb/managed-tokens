package notifications

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/db"
)

// EmailManager is simply a channel on which Notification objects can be sent and received
type EmailManager struct {
	ReceiveChan         chan Notification
	Service             string
	Email               *email
	Database            *db.ManagedTokensDatabase
	NotificationMinimum int
}

type EmailManagerOption func(*EmailManager) error

type serviceErrorCounts struct {
	setupErrors int
	pushErrors  map[string]int
}

// NewServiceEmailManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification, and when EmailManager is closed, will send emails.  Set up the ManagedTokensDatabase and
// the NotificationMinimum via EmailManagerOptions passed in.  If a ManagedTokensDatabase is not passed in via an EmailManagerOption,
// then the EmailManager will send all notifications
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, service string, e *email, opts ...EmailManagerOption) *EmailManager {
	em := &EmailManager{
		ReceiveChan: make(chan Notification),
	}

	for _, opt := range opts {
		if err := opt(em); err != nil {
			log.Errorf("Error running functional option")
		}
	}

	// Get our previous error information for this service
	ec, trackErrorCounts := setErrorCountsByService(ctx, em.Service, em.Database)

	// Set up the various admin channels needed
	adminChan := make(chan Notification)
	adminErrors.writerCount.Add(1)
	go adminErrorAdder(adminChan)

	// Start listening for new notifications
	go func() {
		serviceErrorsTable := make(map[string]string, 0)
		defer wg.Done()
		defer close(adminChan)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					log.WithFields(log.Fields{
						"caller":  "NewServiceEmailManager",
						"service": service,
					}).Error("Timeout exceeded in notification Manager")

				} else {
					log.WithFields(log.Fields{
						"caller":  "NewServiceEmailManager",
						"service": service,
					}).Error(err)
				}
				return

			case n, chanOpen := <-em.ReceiveChan:
				// Channel is closed --> save errors to database and send notifications
				if !chanOpen {
					if err := saveErrorCountsInDatabase(ctx, em.Service, em.Database, ec); err != nil {
						log.WithFields(log.Fields{
							"caller":  "NewEmailManager",
							"service": em.Service,
						}).Error("Error saving new error counts in database.  Please investigate")
					}
					if len(serviceErrorsTable) > 0 {
						tableString := aggregateServicePushErrors(serviceErrorsTable)
						msg, err := prepareServiceEmail(ctx, tableString, e)
						if err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": service,
							}).Error("Error preparing service email for sending")
						}
						if err = SendMessage(ctx, e, msg); err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": service,
							}).Error("Error sending email")
						}
					}
					return
				}
				// Channel is open: direct the message as needed
				shouldSend := true
				if trackErrorCounts {
					shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, ec, em.NotificationMinimum)
				}
				if shouldSend {
					if nValue, ok := n.(*pushError); ok {
						serviceErrorsTable[nValue.node] = n.GetMessage() // TODO This must remain in place
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
	e.templateStruct = struct {
		Timestamp  string
		ErrorTable string
	}{
		Timestamp:  timestamp,
		ErrorTable: errorTable,
	}
	return e.prepareEmailWithTemplate()
}

// TODO Document this.  Bool indicates if we should send a message or not
func adjustErrorCountsByServiceAndDirectNotification(n Notification, ec *serviceErrorCounts, notificationMinimum int) (sendNotification bool) {
	adjustCount := func(count int) (newCount int, shouldSendNotification bool) {
		// If our count is less than the minimum, increment the count and don't send a message
		if count < notificationMinimum {
			// We're under our threshhold for sending notifications, so sendNotification remains false
			newCount = count + 1
		} else {
			// Else, reset the counter to 0, allow for notification to be staged for sending
			count = 0
			sendNotification = true
		}
		return newCount, shouldSendNotification
	}

	if nValue, ok := n.(*pushError); ok {
		// Evaluate the pushError count and change it if needed
		if pushErrorCountVal, pushErrorCountOk := ec.pushErrors[nValue.node]; pushErrorCountOk {
			ec.pushErrors[nValue.node], sendNotification = adjustCount(pushErrorCountVal)
		} else {
			// First time we have an error for this service/node combo. Start the counter, do not send notification
			ec.pushErrors[nValue.node] = 1
		}
		return
	}
	// For setupErrors, if we're tracking the count, examine the current count and change it as needed
	if _, ok := n.(*setupError); ok {
		ec.setupErrors, sendNotification = adjustCount(ec.setupErrors)
	}
	return
}
