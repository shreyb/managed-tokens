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
	"sync"

	"github.com/fermitools/managed-tokens/internal/db"
	log "github.com/sirupsen/logrus"
)

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
	//notificationSourceWg is a waitgroup that keeps track of how many registered notificationSources are sending notifications to this AdminNotificationManager
	notificationSourceWg sync.WaitGroup
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
	a.TrackErrorCounts = shouldTrackErrorCounts

	adminErrorChan := make(chan Notification)                             // Channel to send notifications to aggregator
	startAdminErrorAdder(adminErrorChan)                                  // Start admin errors aggregator concurrently
	runAdminNotificationHandler(ctx, a, adminErrorChan, allServiceCounts) // Start admin notification handler concurrently

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

// registerNotificationSource will return a channel on which callers can send Notifications.  It also spins up a listener goroutine that forwards
// these Notifications to the AdminNotificationManager's ReceiveChan as long as the context is alive
func (a *AdminNotificationManager) registerNotificationSource(ctx context.Context) chan<- Notification {
	c := make(chan Notification)
	a.notificationSourceWg.Add(1)
	go func() {
		defer a.notificationSourceWg.Done()
		for n := range c {
			select {
			case <-ctx.Done():
				return
			default:
				a.ReceiveChan <- n
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
			close(a.ReceiveChan)
			return
		}
	}
}

// startAdminErrorAdder is the function that callers should use to send errors to be aggregated at the notifications package level.
// It allows the caller to specify a channel, adminChan, to send Notifications on.  These errors are added to the global variable adminErrors
func startAdminErrorAdder(adminChan <-chan Notification) {
	adminErrors.writerCount.Add(1)
	go func() {
		defer adminErrors.writerCount.Done()
		for n := range adminChan {
			addErrorToAdminErrors(n)
		}
	}()
}
