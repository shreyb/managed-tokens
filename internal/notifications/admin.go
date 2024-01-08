// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package notifications

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/metrics"
)

// Metrics
var (
	adminErrorNotificationAttemptTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "admin_error_email_last_sent_timestamp",
		Help:      "Last time managed tokens service attempted to send an admin error notification",
	},
		[]string{
			"notification_type",
			"success",
		},
	)
	adminErrorNotificationSendDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "managed_tokens",
		Name:      "admin_error_email_send_duration_seconds",
		Help:      "Time in seconds it took to successfully send an admin error email",
	},
		[]string{
			"notification_type",
		},
	)
)

func init() {
	metrics.MetricsRegistry.MustRegister(adminErrorNotificationAttemptTimestamp)
	metrics.MetricsRegistry.MustRegister(adminErrorNotificationSendDuration)
}

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
	funcLogger := log.WithField("caller", "notifications.NewAdminNotificationManager")

	a := &AdminNotificationManager{
		ReceiveChan:      make(chan Notification), // Channel to send notifications to this Manager
		TrackErrorCounts: true,
		DatabaseReadOnly: true,
	}
	for _, opt := range opts {
		if err := opt(a); err != nil {
			funcLogger.Errorf("Error running functional option")
		}
	}

	// Get our previous error information for this service
	var allServiceCounts map[string]*serviceErrorCounts
	shouldTrackErrorCounts, servicesToTrackErrorCounts := determineIfShouldTrackErrorCounts(ctx, a)
	if shouldTrackErrorCounts {
		allServiceCounts, shouldTrackErrorCounts = getAllErrorCountsFromDatabase(ctx, servicesToTrackErrorCounts, a.Database)
	}

	adminChan := make(chan Notification)                                                     // Channel to send notifications to aggregator
	startAdminErrorAdder(adminChan)                                                          // Start admin errors aggregator concurrently
	runAdminNotificationHandler(ctx, a, adminChan, allServiceCounts, shouldTrackErrorCounts) // Start admin notification handler concurrently

	return a
}

func determineIfShouldTrackErrorCounts(ctx context.Context, a *AdminNotificationManager) (bool, []string) {
	funcLogger := log.WithField("caller", "determineIfShouldTrackErrorCounts")
	if !a.TrackErrorCounts {
		return false, nil
	}
	if a.Database == nil {
		return false, nil
	}

	services, err := a.Database.GetAllServices(ctx)
	if err != nil {
		funcLogger.Error("Error getting services from database.  Assuming that we need to send all notifications")
		return false, nil
	}
	if len(services) == 0 {
		funcLogger.Debug("No services stored in database.  Not counting errors, and will send all notifications")
		return false, nil
	}
	return true, services
}

// getAllErrorCountsFromDatabase gets all the current error counts in the *db.ManagedTokensDatabase for
// every element of services.  If there is an issue doing so, this returns as its second element false, which
// indicates to the caller not to use the returned map
func getAllErrorCountsFromDatabase(ctx context.Context, services []string, database *db.ManagedTokensDatabase) (allServiceCounts map[string]*serviceErrorCounts, valid bool) {
	funcLogger := log.WithField("caller", "notifications.getAllErrorCountsFromDatabase")
	allServiceCounts = make(map[string]*serviceErrorCounts)
	for _, service := range services {
		ec, err := setErrorCountsByService(ctx, service, database)
		if err != nil {
			funcLogger.WithField("service", service).Error("Error setting error count.  Will not use error counts")
			return nil, false
		}
		allServiceCounts[service] = ec
	}
	return allServiceCounts, true
}

// runAdminNotificationHandler handles the routing and counting of errors that result from a
// Notification being sent on the AdminNotificationManager's ReceiveChan
func runAdminNotificationHandler(ctx context.Context, a *AdminNotificationManager, adminChan chan<- Notification, allServiceCounts map[string]*serviceErrorCounts, shouldTrackErrorCounts bool) {
	funcLogger := log.WithField("caller", "runAdminNotificationHandler")

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
					if shouldTrackErrorCounts && !a.DatabaseReadOnly {
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
					if shouldTrackErrorCounts {
						shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, allServiceCounts[n.GetService()], a.NotificationMinimum)
						if !shouldSend {
							funcLogger.Debug("Error count less than error limit.  Not sending notification.")
							continue
						}
					}
					if shouldSend {
						adminChan <- n
					}
				}
			}
		}
	}()
}

// SendAdminNotifications sends admin messages via email and Slack that have been collected in adminErrors. It expects a valid template file
// configured at adminTemplatePath
func SendAdminNotifications(ctx context.Context, operation string, adminTemplatePath string, isTest bool, sendMessagers ...SendMessager) error {
	funcLogger := log.WithField("caller", "notifications.SendAdminNotifications")

	// Let all adminErrors writers finish updating adminErrors
	adminErrors.writerCount.Wait()
	funcLogger.Debug("adminErrors is finalized")

	// No errors - only send slack message saying we tested.  If there are no errors, we don't send emails
	if adminErrorsIsEmpty() {
		if isTest {
			if err := sendSlackNoErrorTestMessages(ctx, sendMessagers); err != nil {
				funcLogger.Error("Error sending admin notifications saying there were no errors in test mode")
				return err
			}
		}
		funcLogger.Debug("No errors to send")
		return nil
	}

	fullMessage, abridgedMessage, err := prepareFullAndAbridgedMessages(operation)
	if err != nil {
		funcLogger.Errorf("Could not prepare full or abridged admin message from template: %s", err)
		return err
	}

	// Run SendMessage on all configured sendMessagers
	g := new(errgroup.Group)
	for _, sm := range sendMessagers {
		sm := sm
		g.Go(func() error {
			var messageToSend, logEnding, metricLabel string
			var successForMetric bool
			switch sm.(type) {
			case *email:
				messageToSend = fullMessage
				logEnding = "admin email"
				metricLabel = "email"
			case *slackMessage:
				messageToSend = abridgedMessage
				logEnding = "slack message"
				metricLabel = "slack_message"
			default:
				err := errors.New("unsupported SendMessager")
				funcLogger.Error(err)
				return err
			}
			start := time.Now()
			err := SendMessage(ctx, sm, messageToSend)
			dur := time.Since(start).Seconds()
			if err != nil {
				funcLogger.Errorf("Failed to send %s", logEnding)
			} else {
				funcLogger.Debugf("Sent %s", logEnding)
				successForMetric = true
				adminErrorNotificationSendDuration.WithLabelValues(metricLabel).Set(dur)
			}
			adminErrorNotificationAttemptTimestamp.WithLabelValues(metricLabel, strconv.FormatBool(successForMetric)).SetToCurrentTime()
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return errors.New("sending admin notifications failed.  Please see logs")
	}

	return nil
}

func sendSlackNoErrorTestMessages(ctx context.Context, sendMessagers []SendMessager) error {
	funcLogger := log.WithField("caller", "sendSlackNoErrorTestMessages")
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
	return nil
}

func prepareFullAndAbridgedMessages(operation string) (fullMessage string, abridgedMessage string, err error) {
	funcLogger := log.WithFields(log.Fields{
		"caller":    "notifications.prepareFullAndAbridgedMessages",
		"operation": operation,
	})
	timestamp := time.Now().Format(time.RFC822)

	// If there are errors, prepare the long-form and abridged messages
	// Prepare the full and abridged messages
	adminErrorsMapFinal := prepareAdminErrorsForFullMessage()
	fullMessageStruct := struct {
		Timestamp   string
		Operation   string
		AdminErrors map[string]AdminDataFinal
		Abridged    bool
	}{
		Timestamp:   timestamp,
		Operation:   operation,
		AdminErrors: adminErrorsMapFinal,
		Abridged:    false,
	}
	fullMessage, err = prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), fullMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare full admin message from template: %s", err)
		return "", "", err
	}

	setupErrorsCombined, pushErrorsCombined := prepareAbridgedAdminSlices()
	abridgedMessageStruct := struct {
		Timestamp           string
		Operation           string
		SetupErrorsCombined []string
		PushErrorsCombined  []string
		Abridged            bool
	}{
		Timestamp:           timestamp,
		Operation:           operation,
		SetupErrorsCombined: setupErrorsCombined,
		PushErrorsCombined:  pushErrorsCombined,
		Abridged:            true,
	}
	abridgedMessage, err = prepareMessageFromTemplate(strings.NewReader(adminErrorsTemplate), abridgedMessageStruct)
	if err != nil {
		funcLogger.Errorf("Could not prepare abridged admin message from template: %s", err)
		return "", "", err
	}
	return fullMessage, abridgedMessage, nil
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
		if data, loaded := adminErrors.errorsMap.LoadOrStore(
			nValue.service,
			&adminData{
				SetupErrors: []string{nValue.message},
			},
		); loaded {
			// Service already has *adminData stored
			if accumulatedAdminData, ok := data.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				// Just append the newest setup error to the slice
				accumulatedAdminData.SetupErrors = append(accumulatedAdminData.SetupErrors, nValue.message)
			}
		}
	// This case is a bit more complicated, since the pushErrors are stored in a sync.Map
	case *pushError:
		data, loaded := adminErrors.errorsMap.LoadOrStore(
			// Roughly an initialization of the PushErrors sync.Map
			nValue.service,
			&adminData{
				PushErrors: sync.Map{},
			},
		)
		if loaded {
			if accumulatedAdminData, ok := data.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				accumulatedAdminData.PushErrors.Store(nValue.node, nValue.message)
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
	funcLogger := log.WithField("caller", "notifications.adminErrorsToAdminDataUnsync")

	adminErrorsMap := make(map[string]*adminData)
	adminErrorsMapUnsync := make(map[string]adminDataUnsync)
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

// adminErrorsIsEmpty checks to see if there are no adminErrors
func adminErrorsIsEmpty() bool { return syncMapLength(adminErrors.errorsMap) == 0 }
