package notifications

import (
	"context"
	"errors"
	"io/ioutil"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
)

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
		var serviceErrorsTable string
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
						msg, err := prepareServiceEmail(ctx, serviceErrorsTable, e)
						if err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": e.service,
							}).Error("Error preparing service email for sending")
						}
						if err = e.SendMessage(ctx, msg); err != nil {
							log.WithFields(log.Fields{
								"caller":  "NewManager",
								"service": e.service,
							}).Error("Error sending email")
						}
					}
					return
				}
				// Channel is open: direct the message as needed
				switch n.NotificationType {
				case SetupError:
					addSetupErrorToAdminErrors(&n)
				case RunError:
					serviceErrorsTable = n.Message
					addTableToAdminErrors(&n)
				}

			}
		}
	}()

	return c
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
	// go func(wg *sync.WaitGroup) {
	// 	defer wg.Done()
	// 	email := &Email{
	// 		From:    nConfig.From,
	// 		To:      nConfig.To,
	// 		Subject: nConfig.Subject,
	// 	}

	// 	if nConfig.IsTest {
	// 		log.Info("This is a test.  Not sending email")
	// 		err = nil
	// 		return
	// 	}

	// 	if err = SendMessage(ctx, email, b.String(), nConfig.ConfigInfo); err != nil {
	// 		log.WithFields(log.Fields{
	// 			"caller":  "SendServiceEmail",
	// 			"service": nConfig.Service,
	// 		}).Error("Failed to send service email")
	// 	}
	// }(&wg)

	// wg.Wait()
	// return b.String(), nil
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminMsgSlice. It expects a valid template file configured at nConfig.ConfigInfo["admin_template"].
// func SendAdminNotifications(ctx context.Context, operation string, SendMessagers []SendMessager ) error {
func SendAdminNotifications(ctx context.Context, operation string, adminTemplatePath string, sendMessagers ...SendMessager) error {
	var wg sync.WaitGroup
	var existsSendError bool
	var b strings.Builder
	adminErrorsMap := make(map[string]AdminData)

	// Convert from sync.Map to Map
	adminErrors.Range(func(service, adminData interface{}) bool {
		e, ok := service.(string)
		if !ok {
			log.Panic("Improper key in admin notifications map.")
		}
		a, ok := adminData.(AdminData)
		if !ok {
			log.Panic("Invalid admin data stored for notification")
		}

		if !a.IsEmpty() {
			adminErrorsMap[e] = a
		}
		return true
	})

	// No errors - only send slack message saying we tested.  If there are no errors, we don't send emails
	if len(adminErrorsMap) == 0 {
		slackMessages := make([]*slackMessage, 0)
		slackMsgText := "Test run completed successfully"
		for _, sm := range sendMessagers {
			if messager, ok := sm.(*slackMessage); ok {
				slackMessages = append(slackMessages, messager)
			}
		}
		for _, slackMessage := range slackMessages {
			if slackErr := slackMessage.SendMessage(ctx, slackMsgText); slackErr != nil {
				log.WithField("caller", "SendAdminNotifications").Error("Failed to send slack message")
				return slackErr
			}
		}
		log.WithField("caller", "SendAdminNotifications").Debug("No errors to send")
		return nil
	}

	timestamp := time.Now().Format(time.RFC822)
	templateData, err := ioutil.ReadFile(adminTemplatePath)
	if err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Could not read admin error template file: %s", err)
		return err
	}
	adminTemplate := template.Must(template.New("admin").Parse(string(templateData)))

	if err = adminTemplate.Execute(&b, struct {
		Timestamp   string
		Operation   string
		AdminErrors map[string]AdminData
	}{
		Timestamp:   timestamp,
		Operation:   operation,
		AdminErrors: adminErrorsMap,
	}); err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Failed to execute admin email template: %s", err)
		return err
	}

	for _, sm := range sendMessagers {
		wg.Add(1)
		go func(sm SendMessager) {
			defer wg.Done()
			if err := sm.SendMessage(ctx, b.String()); err != nil {
				existsSendError = true
				switch sm.(type) {
				case *email:
					log.WithField("caller", "SendAdminNotifications").Error("Failed to send admin email")
				case *slackMessage:
					log.WithField("caller", "SendAdminNotifications").Error("Failed to send slack message")
				default:
					log.WithField("caller", "SendAdminNotifications").Error("Unsupported SendMessager")
				}
			}
		}(sm)
	}

	wg.Wait()

	if existsSendError {
		return errors.New("sending admin notifications failed.  Please see logs")
	}
	return nil
}

// addSetupErrorToAdminErrors adds a Notification.Message to the adminErrors[Notification.Service].SetupErrors slice if the Notifications.NotificationType is SetupError
func addSetupErrorToAdminErrors(n *Notification) {
	// is the service key there?
	// If so, grab the AdminData struct, set it aside
	// Add to that AdminData stuff
	if actual, loaded := adminErrors.LoadOrStore(
		n.Service,
		AdminData{
			SetupErrors: []string{n.Message},
		},
	); loaded {
		if adminData, ok := actual.(AdminData); !ok {
			log.Panic("Invalid data stored in admin errors map.")
		} else {
			adminData.SetupErrors = append(adminData.SetupErrors, n.Message)
			adminErrors.Store(n.Service, adminData)
		}
	}
}

// addTableToAdminErrors populates adminErrors[Notification.Service].RunErrorsTable with the Notification.Message if the Notifications.NotificationType is RunError
func addTableToAdminErrors(n *Notification) {
	if actual, loaded := adminErrors.LoadOrStore(
		n.Service,
		AdminData{RunErrorsTable: n.Message},
	); loaded {
		if adminData, ok := actual.(AdminData); !ok {
			log.Panic("Invalid data stored in admin errors map.")
		} else {
			adminData.RunErrorsTable = n.Message
			adminErrors.Store(n.Service, adminData)
		}
	}
}
