package notifications

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
)

// For Admin notifications, we expect caller to instantiate admin email object, admin slackMessage object, and pass them to SendAdminNotifications.

// packageErrors is a concurrent-safe struct that holds information about all the errors encountered while running package funcs and methods
// It also includes a sync.Mutex and a sync.WaitGroup to coordinate data access
type packageErrors struct {
	errorsMap   sync.Map
	writerCount sync.WaitGroup
	mu          sync.Mutex
}

var (
	// adminErrors holds all the errors to be translated and sent to admins running the various utilities
	adminErrors packageErrors // Store all admin errors here
)

// adminData stores the information needed to generate the admin message
type adminData struct {
	SetupErrors []string
	PushErrors  sync.Map
}

// AdminDataFinal stores the same information as adminData, but with the PushErrors converted to string form, as a table.  The PushErrorsTable field
// is meant to be read as is and used directly in an admin message
type AdminDataFinal struct {
	SetupErrors     []string
	PushErrorsTable string
}

// NewAdminNotificationManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification.  Calling code is expected to run SendAdminNotifications separately to send the accumulated data
// via email (or otherwise)
func NewAdminNotificationManager(ctx context.Context) chan Notification {
	c := make(chan Notification)         // Channel to send notifications to this Manager
	adminChan := make(chan Notification) // Channel to send notifications to aggregator
	adminErrors.writerCount.Add(1)
	go adminErrorAdder(adminChan) // Start admin errors aggregator

	go func() {
		defer close(adminChan)
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					log.WithFields(log.Fields{
						"caller": "NewAdminNotificationManager",
					}).Error("Timeout exceeded in notification Manager")
				} else {
					log.WithFields(log.Fields{
						"caller": "NewAdminNotificationManager",
					}).Error(err)
				}
				return

			case n, chanOpen := <-c:
				// Channel is closed --> send notifications
				if !chanOpen {
					return
				} else {
					// Send notification to admin message aggregator
					adminChan <- n
				}
			}
		}
	}()
	return c
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminMsgSlice. It expects a valid template file
// configured at nConfig.ConfigInfo["admin_template"].
func SendAdminNotifications(ctx context.Context, operation string, adminTemplatePath string, isTest bool, sendMessagers ...SendMessager) error {
	var wg sync.WaitGroup
	var existsSendError bool
	var fullMessageBuilder strings.Builder
	var abridgedMessageBuilder strings.Builder

	adminErrors.writerCount.Wait()
	log.Debug("adminErrors is finalized")

	// No errors - only send slack message saying we tested.  If there are no errors, we don't send emails
	if syncMapLength(adminErrors.errorsMap) == 0 {
		if isTest {
			slackMessages := make([]*slackMessage, 0)
			slackMsgText := "Test run completed successfully"
			for _, sm := range sendMessagers {
				if messager, ok := sm.(*slackMessage); ok {
					slackMessages = append(slackMessages, messager)
				}
			}
			for _, slackMessage := range slackMessages {
				if slackErr := SendMessage(ctx, slackMessage, slackMsgText); slackErr != nil {
					log.WithField("caller", "SendAdminNotifications").Error("Failed to send slack message")
					return slackErr
				}
			}
		}
		log.WithField("caller", "SendAdminNotifications").Debug("No errors to send")
		return nil
	}

	// If there are errors, prepare the long-form and abridged messages
	adminErrorsMapFinal := prepareAdminErrorsForFullMessage()
	setupErrorsCombined, pushErrorsCombined := prepareAbridgedAdminSlices()

	timestamp := time.Now().Format(time.RFC822)
	templateData, err := os.ReadFile(adminTemplatePath)
	if err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Could not read admin error template file: %s", err)
		return err
	}
	adminTemplate := template.Must(template.New("admin").Parse(string(templateData)))
	if err = adminTemplate.Execute(&fullMessageBuilder, struct {
		Timestamp           string
		Operation           string
		AdminErrors         map[string]AdminDataFinal
		SetupErrorsCombined []string
		PushErrorsCombined  []string
		Abridged            bool
	}{
		Timestamp:           timestamp,
		Operation:           operation,
		AdminErrors:         adminErrorsMapFinal,
		SetupErrorsCombined: setupErrorsCombined,
		PushErrorsCombined:  pushErrorsCombined,
		Abridged:            false,
	}); err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Failed to execute full admin template: %s", err)
		return err
	}
	if err = adminTemplate.Execute(&abridgedMessageBuilder, struct {
		Timestamp           string
		Operation           string
		AdminErrors         map[string]AdminDataFinal
		SetupErrorsCombined []string
		PushErrorsCombined  []string
		Abridged            bool
	}{
		Timestamp:           timestamp,
		Operation:           operation,
		AdminErrors:         adminErrorsMapFinal,
		SetupErrorsCombined: setupErrorsCombined,
		PushErrorsCombined:  pushErrorsCombined,
		Abridged:            true,
	}); err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Failed to execute abridged admin template: %s", err)
		return err
	}

	// Run SendMessage on all configured sendMessagers
	for _, sm := range sendMessagers {
		wg.Add(1)
		go func(sm SendMessager) {
			defer wg.Done()
			switch sm.(type) {
			case *email:
				// Send long-form message
				if err := SendMessage(ctx, sm, fullMessageBuilder.String()); err != nil {
					existsSendError = true
					log.WithField("caller", "SendAdminNotifications").Error("Failed to send admin email")
				}
			case *slackMessage:
				// Send abridged message
				if err := SendMessage(ctx, sm, abridgedMessageBuilder.String()); err != nil {
					existsSendError = true
					log.WithField("caller", "SendAdminNotifications").Error("Failed to send slack message")
				}
			default:
				log.WithField("caller", "SendAdminNotifications").Error("Unsupported SendMessager")
			}
		}(sm)
	}
	wg.Wait()

	if existsSendError {
		return errors.New("sending admin notifications failed.  Please see logs")
	}
	return nil
}

// prepareAbridgedAdminSlices takes the stored adminErrors and returns two []string objects containing the various setup and push errors, not broken
// up by service.  This is for abridged messages like slack messages.
func prepareAbridgedAdminSlices() (setupErrorsCombined []string, pushErrorsCombined []string) {
	adminErrorsUnsync := adminErrorsToAdminDataUnsync()
	for service, data := range adminErrorsUnsync {
		for _, setupError := range data.SetupErrors {
			setupErrorsCombined = append(setupErrorsCombined, fmt.Sprintf("%s: %s", service, setupError))
		}
		for node, pushError := range data.PushErrors {
			pushErrorsCombined = append(pushErrorsCombined, fmt.Sprintf("%s@%s: %s", service, node, pushError))
		}
	}
	return setupErrorsCombined, pushErrorsCombined
}

func adminErrorAdder(adminChan <-chan Notification) {
	defer adminErrors.writerCount.Done()
	for n := range adminChan {
		addErrorToAdminErrors(n)
	}
}

// addErrorToAdminErrors takes the passed in Notification, type-checks it, and adds it to the appropriate field of adminErrors
func addErrorToAdminErrors(n Notification) {
	adminErrors.mu.Lock()
	defer adminErrors.mu.Unlock()

	switch nValue := n.(type) {
	case *setupError:
		if actual, loaded := adminErrors.errorsMap.LoadOrStore(
			nValue.service,
			adminData{
				SetupErrors: []string{nValue.message},
			},
		); loaded {
			if adminData, ok := actual.(adminData); !ok {
				log.Panic("Invalid data stored in admin errors map.")
			} else {
				adminData.SetupErrors = append(adminData.SetupErrors, nValue.message)
				adminErrors.errorsMap.Store(nValue.service, adminData)
			}
		}
	case *pushError:
		var newSyncMap sync.Map
		newSyncMap.Store(nValue.node, nValue.message)
		if actual, loaded := adminErrors.errorsMap.LoadOrStore(
			nValue.service,
			adminData{
				PushErrors: newSyncMap,
			},
		); loaded {
			if adminData, ok := actual.(adminData); !ok {
				log.Panic("Invalid data stored in admin errors map.")
			} else {
				adminData.PushErrors.Store(nValue.node, nValue.message)
				adminErrors.errorsMap.Store(n.GetService(), adminData)
			}
		}
	}
}

// prepareAdminErrorsForMessage transforms the accumulated adminErrors variable from type adminData to AdminDataFinal
func prepareAdminErrorsForFullMessage() map[string]AdminDataFinal {
	adminErrorsIntermediateMap := adminErrorsToAdminDataUnsync()
	adminErrorsMapFinal := make(map[string]AdminDataFinal)

	// Convert pushErrors map to string, save as map adminErrorsMapFinal so we get our final form.
	for service, aData := range adminErrorsIntermediateMap {
		a := AdminDataFinal{
			SetupErrors: aData.SetupErrors,
			PushErrorsTable: PrepareTableStringFromMap(
				aData.PushErrors,
				"The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:",
				[]string{"Node", "Error"},
			),
		}
		adminErrorsMapFinal[service] = a

	}
	return adminErrorsMapFinal

}

// adminDataUnsync is an intermediate data structure between adminData and AdminDataFinal that translates the adminData.PushErrors sync.Map to a regular map[string]string
type adminDataUnsync struct {
	SetupErrors []string
	PushErrors  map[string]string
}

func adminErrorsToAdminDataUnsync() map[string]adminDataUnsync {
	adminErrorsMap := make(map[string]adminData)
	adminErrorsMapUnsync := make(map[string]adminDataUnsync)
	// 1.  Write adminErrors from sync.Map to Map called adminErrorsMap
	adminErrors.errorsMap.Range(func(service, aData any) bool {
		s, ok := service.(string)
		if !ok {
			log.Panic("Improper key in admin notifications map.")
		}
		a, ok := aData.(adminData)
		if !ok {
			log.Panic("Invalid admin data stored for notification")
		}

		if !a.isEmpty() {
			adminErrorsMap[s] = a
		}
		return true
	})

	// 2. Take adminErrorsMap, convert so that values are adminErrorUnsync objects.
	for service, aData := range adminErrorsMap {
		a := adminDataUnsync{
			SetupErrors: aData.SetupErrors,
			PushErrors:  make(map[string]string),
		}
		aData.PushErrors.Range(func(node, err any) bool {
			n, ok := node.(string)
			if !ok {
				log.Panic("Improper key in push errors map")
			}
			e, ok := err.(string)
			if !ok {
				log.Panic("Improper error string in push errors map")
			}

			if e != "" {
				a.PushErrors[n] = e
			}
			return true
		})
		adminErrorsMapUnsync[service] = a
	}
	return adminErrorsMapUnsync
}

// isEmpty checks to see if a variable of type adminData has any data
func (a *adminData) isEmpty() bool {
	return ((len(a.SetupErrors) == 0) && (syncMapLength(a.PushErrors) == 0))
}
