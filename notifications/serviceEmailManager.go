package notifications

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ServiceEmailManager is simply a channel on which Notification objects can be sent and received
type ServiceEmailManager chan Notification

// Some notes to go away later.
// . We expect callers to call NewManager if they want to run for real,and send notifications which either have single setuperrors, or the aggregated
// list of runerrors
// Caller should also instantiate email object with NewEmail(), pass in here
//
// For Admin notifications, we expect caller to instantiate admin email object, admin slackMessage object, and pass them in.  Caller will handle case of
// whether it's running a test or not by setting the "to" field in the *email object

// NewServiceEmailManager returns a ServiceEmailManager channel for callers to send Notifications on.  It will collect messages, and when Manager is closed, will send emails, depending on nConfig.IsTest
func NewServiceEmailManager(ctx context.Context, wg *sync.WaitGroup, e *email) ServiceEmailManager {
	// func NewManager(ctx context.Context, wg *sync.WaitGroup, nConfig Config) Manager {
	var isTest bool
	c := make(ServiceEmailManager)

	// If the passed in *email object has a blank recipients list, we consider this a test run
	if len(e.to) == 0 {
		log.Info("Recipients list is empty - assuming we are running in test mode, and will not send service emails")
		isTest = true
	}

	go func() {
		// var serviceErrorsTable string
		serviceErrorsTable := make(map[string]string, 0)
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					log.WithFields(log.Fields{
						"caller":  "NewManager",
						"service": e.service,
					}).Error("Timeout exceeded in notification Manager")

				} else {
					log.WithFields(log.Fields{
						"caller":  "NewManager",
						"service": e.service,
					}).Error(err)
				}
				return

			case n, chanOpen := <-c:
				// Channel is closed --> send notifications
				if !chanOpen {
					if isTest {
						return
					}
					// If running for real, send the service email
					if len(serviceErrorsTable) > 0 {
						// if err := SendServiceEmail(ctx, nConfig, exptErrorTable); err != nil {
						tableString := aggregateServicePushErrors(serviceErrorsTable)
						msg, err := prepareServiceEmail(ctx, tableString, e)
						if err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": e.service,
							}).Error("Error preparing service email for sending")
						}
						if err = SendMessage(ctx, e, msg); err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": e.service,
							}).Error("Error sending email")
						}
					}
					return
				}
				// Channel is open: direct the message as needed
				if nValue, ok := n.(*pushError); ok {
					serviceErrorsTable[nValue.node] = n.GetMessage()
				}
				addErrorToAdminErrors(n)
			}
		}
	}()

	return c
}

func aggregateServicePushErrors(servicePushErrors map[string]string) string {
	helpText := "The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:"
	header := []string{"Node", "Error"}
	return PrepareTableStringFromMap(servicePushErrors, helpText, header)
}

// SendServiceEmail sends an service-specific error message email based on nConfig.  It expects a valid template file configured at notifications.service_template
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
