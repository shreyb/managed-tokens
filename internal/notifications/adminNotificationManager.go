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
	"sync"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/fermitools/managed-tokens/internal/tracing"
)

// AdminNotificationManager holds information needed to receive and handle notifications meant to be sent to the administrators of the managed
// tokens utilities.
type AdminNotificationManager struct {
	// receiveChan is the channel on which callers should send Notifications to be forwarded to administrators.  Callers should use GetReceiveChan() and RequestToCloseReceiveChan() to interact with this channel
	receiveChan          chan Notification
	closeReceiveChanOnce sync.Once // sync.Once to ensure that we only close ReceiveChan once through RequestToCloseReceiveChan
	// Database is the underlying *db.ManagedTokensDatabase that will be queried by the AdminNotificationManager to determine whether
	// or not to send a particular Notification received on the ReceiveChan to administrators
	Database *db.ManagedTokensDatabase
	// NotificationMinimum is the minimum number of prior similar Notifications required for an AdminNotificationManager to determine that it should
	// send a Notification to administrators
	NotificationMinimum int
	// TrackErrorCounts determines whether or not the AdminNotificationManager should consult the Database or not when deciding whether to send a
	// Notification sent on its ReceiveChan.  If set to false, all received Notifications will be sent to administrators.
	// This should only be set to true if there are no other possible decision-makers like callers that call registerNotificationSource
	TrackErrorCounts bool
	// DatabaseReadOnly determines whether the AdminNotificationManager should write its error counts to the db.ManagedTokensDatabase after finishing
	// all processing.  This should only be set to false if there are no other possible database writers (like ServiceEmailManager), to avoid double-counting
	// of errors
	DatabaseReadOnly bool
	//notificationSourceWg is a waitgroup that keeps track of how many registered notificationSources are sending notifications to this AdminNotificationManager
	notificationSourceWg sync.WaitGroup
	// adminErrorChan is the channel on which all errors handled by this type should be forwarded to be added to the package's global var adminErrors
	adminErrorChan chan Notification
	// allServiceCounts holds the service error counts for all the services being managed during this run
	allServiceCounts map[string]*serviceErrorCounts
}

// NewAdminNotificationManager returns an EmailManager channel for callers to send Notifications on.  It will collect messages and sort them according
// to the underlying type of the Notification.  Calling code is expected to run SendAdminNotifications separately to send the accumulated data
// via email (or otherwise).  Functional options should be specified to set the fields (see AdminNotificationManagerOption documentation).
// This function should never be called more than once concurrently.
func NewAdminNotificationManager(ctx context.Context, opts ...AdminNotificationManagerOption) *AdminNotificationManager {
	ctx, span := tracer.Start(ctx, "NewAdminNotificationManager", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	funcLogger := log.WithField("caller", "notifications.NewAdminNotificationManager")

	a := &AdminNotificationManager{
		receiveChan:      make(chan Notification), // Channel to send notifications to this Manager
		DatabaseReadOnly: true,
	}
	for _, opt := range opts {
		aBackup := backupAdminNotificationManager(a)
		if err := opt(a); err != nil {
			funcLogger.Errorf("Error running functional option")
			a = aBackup
		}
	}

	// Get our previous error information for this service
	a.allServiceCounts = make(map[string]*serviceErrorCounts)
	shouldTrackErrorCounts, servicesToTrackErrorCounts := determineIfShouldTrackErrorCounts(ctx, a)
	if shouldTrackErrorCounts {
		a.allServiceCounts, shouldTrackErrorCounts = getAllErrorCountsFromDatabase(ctx, servicesToTrackErrorCounts, a.Database)
	}
	a.TrackErrorCounts = shouldTrackErrorCounts
	funcLogger.Debugf("AdminNotificationManager.TrackErrorCounts: %t", a.TrackErrorCounts)
	funcLogger.Debugf("AdminNotificationManager.DatabaseReadOnly: %t", a.DatabaseReadOnly)

	a.adminErrorChan = make(chan Notification) // Channel to send notifications to aggregator
	a.startAdminErrorAdder()                   // Start admin errors aggregator concurrently
	a.runAdminNotificationHandler(ctx)         // Start admin notification handler concurrently

	return a
}

// backupAdminNotificationManager should ONLY be used to back up an AdminNotificationManager before it is being used (that is, during the initialization
// of the AdminNotificationManager).  For example, it can be used while applying functional opts to mutate the AdminNotificationManager
func backupAdminNotificationManager(a1 *AdminNotificationManager) *AdminNotificationManager {
	a2 := new(AdminNotificationManager)

	a2.receiveChan = make(chan Notification)
	a2.closeReceiveChanOnce = sync.Once{}
	a2.notificationSourceWg = sync.WaitGroup{}
	a2.adminErrorChan = make(chan Notification)

	a2.Database = a1.Database
	a2.NotificationMinimum = a1.NotificationMinimum
	a2.TrackErrorCounts = a1.TrackErrorCounts
	a2.DatabaseReadOnly = a1.DatabaseReadOnly
	a2.allServiceCounts = a1.allServiceCounts

	return a2
}

func determineIfShouldTrackErrorCounts(ctx context.Context, a *AdminNotificationManager) (bool, []string) {
	ctx, span := tracer.Start(ctx, "determineIfShouldTrackErrorCounts")
	defer span.End()

	funcLogger := log.WithField("caller", "determineIfShouldTrackErrorCounts")
	if !a.TrackErrorCounts {
		return false, nil
	}
	if a.Database == nil {
		return false, nil
	}

	services, err := a.Database.GetAllServices(ctx)
	if err != nil {
		err = fmt.Errorf("error getting services from database: %w", err)
		tracing.LogErrorWithTrace(span, err)
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
	ctx, span := tracer.Start(ctx, "getAllErrorCountsFromDatabase")
	defer span.End()

	allServiceCounts = make(map[string]*serviceErrorCounts)
	for _, service := range services {
		ec, err := setErrorCountsByService(ctx, service, database)
		if err != nil {
			err = fmt.Errorf("error getting error counts from database: %w", err)
			tracing.LogErrorWithTrace(span, err)
			return nil, false
		}
		allServiceCounts[service] = ec
	}
	return allServiceCounts, true
}

// runAdminNotificationHandler handles the routing and counting of errors that result from a
// Notification being sent on the AdminNotificationManager's ReceiveChan
func (a *AdminNotificationManager) runAdminNotificationHandler(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "runAdminNotificationHandler", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	funcLogger := log.WithField("caller", "runAdminNotificationHandler")

	go func() {
		defer close(a.adminErrorChan)
		ctx, span := tracer.Start(ctx, "runAdminNotificationHandler_anonFunc", trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()
		for {
			select {
			case <-ctx.Done():
				if err := ctx.Err(); err == context.DeadlineExceeded {
					err = errors.New("timeout exceeded in notification Manager")
					tracing.LogErrorWithTrace(span, err)
					funcLogger.Error(err.Error())
				} else {
					tracing.LogErrorWithTrace(span, errors.New("context canceled in notification Manager"))
					funcLogger.Error(err)
				}
				return

			case n, chanOpen := <-a.receiveChan:
				// Channel is closed --> send notifications
				if !chanOpen {
					// Only save error counts if we expect no other NotificationsManagers (like ServiceEmailManager) to write to the database
					if a.TrackErrorCounts && !a.DatabaseReadOnly {
						for service, ec := range a.allServiceCounts {
							if err := saveErrorCountsInDatabase(ctx, service, a.Database, ec); err != nil {
								funcLogger.WithField("service", n.GetService()).Error("Error saving new error counts in database.  Please investigate")
							}
						}
					}
					return
				} else {
					funcLogger.WithFields(log.Fields{
						"service": n.GetService(),
						"message": n.GetMessage(),
					}).Debug("Received notification message")
					// Send notification to admin message aggregator
					shouldSend := true
					// If we got a SourceNotification, don't run any of the following checks.  Just forward it on the adminErrorChan
					if val, ok := n.(SourceNotification); ok {
						n = val.Notification
					} else {
						if a.TrackErrorCounts {
							a.verifyServiceErrorCounts(n.GetService())
							shouldSend = adjustErrorCountsByServiceAndDirectNotification(n, a.allServiceCounts[n.GetService()], a.NotificationMinimum)
							if !shouldSend {
								funcLogger.Debug("Error count less than error limit.  Not sending notification.")
								continue
							}
						}
					}
					if shouldSend {
						a.adminErrorChan <- n
					}
				}
			}
		}
	}()
}

// SourceNotification is a type containing a Notification.  It should be used to send notifications from callers that are sending Notifications to the
// AdminNotificationManager via a channel gotten via registerNotificationSource.  The notifications/admin package will send all of these types of
// Notifications - that is, it will not run any checks to determine whether a SourceNotification should be sent.
type SourceNotification struct {
	Notification
}

// registerNotificationSource will return a channel on which callers can send SourceNotifications.  It also spins up a listener goroutine that forwards
// these Notifications to the AdminNotificationManager's ReceiveChan as long as the context is alive
func (a *AdminNotificationManager) RegisterNotificationSource(ctx context.Context) chan<- SourceNotification {
	ctx, span := tracer.Start(ctx, "registerNotificationSource")
	defer span.End()

	c := make(chan SourceNotification)
	a.notificationSourceWg.Add(1)
	go func() {
		defer a.notificationSourceWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case n, chanOpen := <-c:
				if !chanOpen {
					return
				}
				a.receiveChan <- n
			}
		}
	}()
	return c
}

// RequestToCloseReceiveChan will wait until either all notificationSources have finished sending Notifications, or until the context expires.
// If the former happens, then the AdminNotificationsManager's ReceiveChan will be closed.  Otherwise, the function will return without closing
// the channel (allowing the program to exit without sending notifications).  In the case of multiple goroutines sending notifications to
// the AdminNotificationManager, this method is how the ReceiveChan should be closed.
func (a *AdminNotificationManager) RequestToCloseReceiveChan(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "RequestToCloseReceiveChan")
	defer span.End()

	c := make(chan struct{})
	go func() {
		a.notificationSourceWg.Wait()
		close(c)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c:
			a.closeReceiveChanOnce.Do(func() { close(a.receiveChan) })
			return
		}
	}
}

// startAdminErrorAdder is the function that callers should use to send errors to be aggregated at the notifications package level.
// It allows the caller to specify a channel, adminChan, to send Notifications on.  These errors are added to the global variable adminErrors
func (a *AdminNotificationManager) startAdminErrorAdder() {
	adminErrors.writerCount.Add(1)
	go func() {
		defer adminErrors.writerCount.Done()
		for n := range a.adminErrorChan {
			addErrorToAdminErrors(n)
		}
	}()
}

// verifyServiceErrorCounts checks allServiceCounts for the existence of a service key.  If it does exist, true is returned.
// If it does not exist, a new entry in allServiceCounts is created with an initialized *serviceErrorCounts.pushErrors map, and false is returned
func (a *AdminNotificationManager) verifyServiceErrorCounts(service string) bool {
	var ec *serviceErrorCounts
	if _, ok := a.allServiceCounts[service]; !ok {
		ec = &serviceErrorCounts{
			pushErrors: make(map[string]errorCount),
		}
		a.allServiceCounts[service] = ec
		return false
	}
	return true
}
