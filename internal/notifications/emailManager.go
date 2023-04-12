package notifications

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// EmailManager is simply a channel on which Notification objects can be sent and received
type EmailManager chan Notification

// NewServiceEmailManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification, and when EmailManager is closed, will send emails
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, service string, e *email) EmailManager {
	c := make(EmailManager)
	adminChan := make(chan Notification)
	adminErrors.writerCount.Add(1)
	go adminErrorAdder(adminChan)

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

			case n, chanOpen := <-c:
				// Channel is closed --> send notifications
				if !chanOpen {
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
				if nValue, ok := n.(*pushError); ok {
					serviceErrorsTable[nValue.node] = n.GetMessage()
				}
				adminChan <- n
			}
		}
	}()

	return c
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
