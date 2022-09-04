package notifications

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/sirupsen/logrus"
)

type packageErrors struct {
	errorsMap   sync.Map
	writerCount sync.WaitGroup
	mu          sync.Mutex
}

var (
	adminErrors packageErrors // Store all admin errors here
)

// AdminData stores the information needed to generate the Admin notifications
type adminData struct {
	SetupErrors []string
	PushErrors  sync.Map
}

type AdminDataFinal struct {
	SetupErrors     []string
	PushErrorsTable string
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminMsgSlice. It expects a valid template file configured at nConfig.ConfigInfo["admin_template"].
// func SendAdminNotifications(ctx context.Context, operation string, SendMessagers []SendMessager ) error {
func SendAdminNotifications(ctx context.Context, operation string, adminTemplatePath string, isTest bool, sendMessagers ...SendMessager) error {
	var wg sync.WaitGroup
	var existsSendError bool
	var b strings.Builder

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

	adminErrorsMapFinal := prepareAdminErrorsForMessage()

	timestamp := time.Now().Format(time.RFC822)
	templateData, err := os.ReadFile(adminTemplatePath)
	if err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Could not read admin error template file: %s", err)
		return err
	}
	adminTemplate := template.Must(template.New("admin").Parse(string(templateData)))

	if err = adminTemplate.Execute(&b, struct {
		Timestamp   string
		Operation   string
		AdminErrors map[string]AdminDataFinal
	}{
		Timestamp:   timestamp,
		Operation:   operation,
		AdminErrors: adminErrorsMapFinal,
	}); err != nil {
		log.WithField("caller", "SendAdminNotifications").Errorf("Failed to execute admin email template: %s", err)
		return err
	}

	for _, sm := range sendMessagers {
		wg.Add(1)
		go func(sm SendMessager) {
			defer wg.Done()
			if err := SendMessage(ctx, sm, b.String()); err != nil {
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

func adminErrorAdder(adminChan <-chan Notification) {
	defer adminErrors.writerCount.Done()
	for n := range adminChan {
		addErrorToAdminErrors(n)
	}
}

// addErrorToAdminErrors takes the passed in Notification, type-checks it, and adds it to the appropriate field of adminErrors
func addErrorToAdminErrors(n Notification) {
	// is the service key there?
	// If so, grab the AdminData struct, set it aside
	// Add to that AdminData stuff
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

func prepareAdminErrorsForMessage() map[string]AdminDataFinal {

	// Intermediate data structure between AdminData and AdminDataFinal
	type adminData2 struct {
		SetupErrors []string
		PushErrors  map[string]string
	}

	adminErrorsMap := make(map[string]adminData)
	adminErrorsMap2 := make(map[string]adminData2)
	adminErrorsMapFinal := make(map[string]AdminDataFinal)

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

	// 2. Take adminErrorsMap, convert to intermediate map with PushErrors as a regular map[string]string.  Map saved as adminErrorsMap2
	for service, aData := range adminErrorsMap {
		a := adminData2{
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
		adminErrorsMap2[service] = a
	}

	// Final pass-through:  Convert pushErrors map to string, save as map adminErrorsMapFinal so we get our final form.
	for service, aData := range adminErrorsMap2 {
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

func (a *adminData) isEmpty() bool {
	return ((len(a.SetupErrors) == 0) && (syncMapLength(a.PushErrors) == 0))
}
