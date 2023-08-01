package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/shreyb/managed-tokens/internal/db"
)

// For Admin notifications, we expect caller to instantiate admin email object, admin slackMessage object, and pass them to SendAdminNotifications.

// packageErrors is a concurrent-safe struct that holds information about all the errors encountered while running package funcs and methods
// It also includes a sync.Mutex and a sync.WaitGroup to coordinate data access
type packageErrors struct {
	errorsMap   *sync.Map      // Holds all the errors accumulated for the current invocation.  Roughly a map[service string]*adminData
	writerCount sync.WaitGroup // WaitGroup to be incremented anytime a function wants to write to a packageErrors
	mu          sync.Mutex
}

var (
	// adminErrors holds all the errors to be translated and sent to admins running the various utilities.
	// Callers should increment the writerCount waitgroup upon starting up, and decrement when they return.
	adminErrors packageErrors
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

// AdminNotificationManager holds information needed to receive and handle notifications meant to be sent to the administrators of the managed
// tokens utilities.
type AdminNotificationManager struct {
	ReceiveChan chan Notification // ReceiveChan is the channel on which callers should send Notifications to be forwarded to administrators
	// Database is the underlying *db.ManagedTokensDatabase that will be queried by the AdminNotificationManager to determine whether
	// or not to send a particular Notification received on the ReceiveChan to administrators
	Database *db.ManagedTokensDatabase
	// NotificationMinimum is the minimum number of prior similar Notifications required for an AdminNotificationManager to determine that it should
	// send a Notification to administrators
	NotificationMinimum int
	// TrackErrorCounts determines whether or not the AdminNotificationManager should consult the Database or not.  If set to false, all received
	// Notifications will be sent to administrators
	TrackErrorCounts bool
	// DatabaseReadOnly determines whether the AdminNotificationManager should write its error counts to the db.ManagedTokensDatabase after finishing
	// all processing.  This should only be set to false if there are no other possible database writers (like ServiceEmailManager), to avoid double-counting
	// of errors
	DatabaseReadOnly bool
}

// AdminNotificationManagerOption is a functional option that should be used as an argument to NewAdminNotificationManager to set various fields
// of the AdminNotificationManager
// For example:
//
//	 f := func(a *AdminNotificationManager) error {
//		  a.NotificationMinimum = 42
//	   return nil
//	 }
//	 g := func(a *AdminNotificationManager) error {
//		  a.DatabaseReadOnly = false
//	   return nil
//	 }
//	 manager := NewAdminNotificationManager(context.Background, f, g)
type AdminNotificationManagerOption func(*AdminNotificationManager) error

// NewAdminNotificationManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification.  Calling code is expected to run SendAdminNotifications separately to send the accumulated data
// via email (or otherwise).  Functional options should be specified to set the fields (see AdminNotificationManagerOption documentation)
func NewAdminNotificationManager(ctx context.Context, opts ...AdminNotificationManagerOption) *AdminNotificationManager {
	var trackErrorCounts bool = true
	services := make([]string, 0)
	a := &AdminNotificationManager{
		ReceiveChan:      make(chan Notification), // Channel to send notifications to this Manager
		TrackErrorCounts: trackErrorCounts,
		DatabaseReadOnly: true,
	}
	funcLogger := log.WithField("caller", "notifications.NewAdminNotificationManager")

	for _, opt := range opts {
		if err := opt(a); err != nil {
			funcLogger.Errorf("Error running functional option")
		}
	}

	// Get our previous error information for this service
	allServiceCounts := make(map[string]*serviceErrorCounts)
	if !a.TrackErrorCounts {
		trackErrorCounts = false
	}
	if a.Database == nil {
		trackErrorCounts = false
	} else {
		var err error
		services, err = a.Database.GetAllServices(ctx)
		if err != nil {
			funcLogger.Error("Error getting services from database.  Assuming that we need to send all notifications")
			trackErrorCounts = false
		}
	}
	if len(services) == 0 {
		funcLogger.Debug("No services stored in database.  Not counting errors, and will send all notifications")
		trackErrorCounts = false
	}

	if trackErrorCounts {
		for _, service := range services {
			ec, trackErrorCountsByService := setErrorCountsByService(ctx, service, a.Database)
			allServiceCounts[service] = ec
			if !trackErrorCountsByService {
				trackErrorCounts = false
			}
		}
	}

	adminChan := make(chan Notification) // Channel to send notifications to aggregator
	startAdminErrorAdder(adminChan)      // Start admin errors aggregator

	go func() {
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

			case n, chanOpen := <-a.ReceiveChan:
				// Channel is closed --> send notifications
				if !chanOpen {
					// Only save error counts if we expect no other NotificationsManagers (like ServiceEmailManager) to write to the database
					if trackErrorCounts && !a.DatabaseReadOnly {
						for service, ec := range allServiceCounts {
							if err := saveErrorCountsInDatabase(ctx, service, a.Database, ec); err != nil {
								funcLogger.WithField("service", n.GetService()).Error("Error saving new error counts in database.  Please investigate")
							}
						}
					}
					return
				} else {
					// Send notification to admin message aggregator
					shouldSend := true
					if trackErrorCounts {
						shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, allServiceCounts[n.GetService()], a.NotificationMinimum)
						if !shouldSend {
							funcLogger.Debug("Error count less than error limit.  Not sending notification.")
						}
					}
					if shouldSend {
						adminChan <- n
					}
				}
			}
		}
	}()
	return a
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminErrors. It expects a valid template file
// configured at adminTemplatePath
func SendAdminNotifications(ctx context.Context, operation string, adminTemplatePath string, isTest bool, sendMessagers ...SendMessager) error {
	var wg sync.WaitGroup
	var existsSendError bool
	funcLogger := log.WithField("caller", "notifications.SendAdminNotifications")

	adminErrors.writerCount.Wait()
	funcLogger.Debug("adminErrors is finalized")

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
					funcLogger.Error("Failed to send slack message")
					return slackErr
				}
			}
		}
		funcLogger.Debug("No errors to send")
		return nil
	}

	// If there are errors, prepare the long-form and abridged messages
	adminErrorsMapFinal := prepareAdminErrorsForFullMessage()
	setupErrorsCombined, pushErrorsCombined := prepareAbridgedAdminSlices()

	// Prepare the full and abridged messages
	timestamp := time.Now().Format(time.RFC822)
	fullMessageStruct := struct {
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
	}
	abridgedMessageStruct := struct {
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
	}

	fullMessage, err := prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), fullMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare full admin message from template: %s", err)
		return err
	}
	abridgedMessage, err := prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), abridgedMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare abridged admin message from template: %s", err)
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
				if err := SendMessage(ctx, sm, fullMessage); err != nil {
					existsSendError = true
					funcLogger.Error("Failed to send admin email")
				}
			case *slackMessage:
				// Send abridged message
				if err := SendMessage(ctx, sm, abridgedMessage); err != nil {
					existsSendError = true
					funcLogger.Error("Failed to send slack message")
				}
			default:
				funcLogger.Error("Unsupported SendMessager")
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

// startAdminErrorAdder is the function that most callers should use to send errors to the admin message handlers.  It allows the caller
// to specify a channel, adminChan, to send Notifications on.  These Notifications are forwarded to the admin message handlers and
// sorted appropriately.  Callers should
func startAdminErrorAdder(adminChan <-chan Notification) {
	adminErrors.writerCount.Add(1)
	go func() {
		defer adminErrors.writerCount.Done()
		for n := range adminChan {
			addErrorToAdminErrors(n)
		}
	}()
}

// addErrorToAdminErrors takes the passed in Notification, type-checks it, and adds it to the appropriate field of adminErrors
func addErrorToAdminErrors(n Notification) {
	adminErrors.mu.Lock()
	funcLogger := log.WithField("caller", "notifications.addErrorToAdminErrors")
	defer adminErrors.mu.Unlock()

	// The first time addErrorToAdminErrors is called, initialize the errorsMap so we don't get a nil pointer dereference panic
	// later on when we try to check the sync.Map for values
	if adminErrors.errorsMap == nil {
		m := sync.Map{}
		adminErrors.errorsMap = &m
	}

	switch nValue := n.(type) {
	// For *setupErrors, store or append the setupError text to the appropriate field
	case *setupError:
		if actual, loaded := adminErrors.errorsMap.LoadOrStore(
			nValue.service,
			&adminData{
				SetupErrors: []string{nValue.message},
			},
		); loaded {
			// Service already has *adminData stored
			if accumulatedAdminData, ok := actual.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				// Just append the newest setup error to the slice
				accumulatedAdminData.SetupErrors = append(accumulatedAdminData.SetupErrors, nValue.message)
			}
		}
	// This case is a bit more complicated, since the pushErrors are stored in a sync.Map
	case *pushError:
		actual, loaded := adminErrors.errorsMap.LoadOrStore(
			// Roughly an initialization of the PushErrors sync.Map
			nValue.service,
			&adminData{
				PushErrors: sync.Map{},
			},
		)
		if loaded {
			if adminData, ok := actual.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				adminData.PushErrors.Store(nValue.node, nValue.message)
			}
		} else {
			// At this point, since we didn't wrap the LoadOrStore call in an if-contraction (if <expression>; loaded {})
			// we know that if loaded == false, then adminErrors.errorsMap[nValue.service] = &adminData{PushErrors: sync.Map{}}
			// from above.  So all we need to do is load the pointer value, type-check it, and store our message.
			//
			// We need to do it this way because otherwise, we'd have to instantiate a sync.Map with the values stored, and then
			// copy it into adminErrors, which copies the underlying mutex.  That could lead to concurrency issues later.
			if accumulatedAdminData, ok := adminErrors.errorsMap.Load(nValue.service); ok {
				if accumulatedAdminDataVal, ok := accumulatedAdminData.(*adminData); ok {
					accumulatedAdminDataVal.PushErrors.Store(nValue.node, nValue.message)
				}
			}
		}
	}
}

// prepareAdminErrorsForMessage transforms the accumulated adminErrors variable from type *adminData to AdminDataFinal
func prepareAdminErrorsForFullMessage() map[string]AdminDataFinal {
	// Get our adminErrors from type map[string]*adminData to a map[string]AdminDataUnsync so it's easier to work with
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

// adminDataUnsync is an intermediate data structure between *adminData and AdminDataFinal that translates the adminData.PushErrors sync.Map
// to a regular map[string]string
type adminDataUnsync struct {
	SetupErrors []string
	PushErrors  map[string]string
}

// adminErrorsToAdminDataUnsync translates the accumulated adminErrors.errorsMap into a map[string]adminDataUnsync so that
// we have easier access to the structure of the data
func adminErrorsToAdminDataUnsync() map[string]adminDataUnsync {
	adminErrorsMap := make(map[string]*adminData)
	adminErrorsMapUnsync := make(map[string]adminDataUnsync)
	funcLogger := log.WithField("caller", "notifications.adminErrorsToAdminDataUnsync")
	// 1.  Write adminErrors from sync.Map to Map called adminErrorsMap
	adminErrors.errorsMap.Range(func(service, aData any) bool {
		s, ok := service.(string)
		if !ok {
			funcLogger.Panic("Improper key in admin notifications map.")
		}
		a, ok := aData.(*adminData)
		if !ok {
			funcLogger.Panic("Invalid admin data stored for notification")
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
				funcLogger.Panic("Improper key in push errors map")
			}
			e, ok := err.(string)
			if !ok {
				funcLogger.Panic("Improper error string in push errors map")
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
	return ((len(a.SetupErrors) == 0) && (syncMapLength(&a.PushErrors) == 0))
}
