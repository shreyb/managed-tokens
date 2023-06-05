package notifications

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/shreyb/managed-tokens/internal/db"
	log "github.com/sirupsen/logrus"
)

// errorCount is an integer that keeps track of whether its value was changed from when it was initially loaded
type errorCount struct {
	value   int
	changed bool
}

// set sets the value of the errorCount and tells the errorCount that its value was changed form the initial loading.  This is the
// preferred way of changing the value of the errorCount
func (ec *errorCount) set(val int) {
	ec.value = val
	ec.changed = true
}

// serviceErrorCounts keeps track of the number of errors a given service has registered, both prior, and during the current run.
type serviceErrorCounts struct {
	setupErrors errorCount // setupErrors is simply an errorCount keeping track of how many *setupErrors have been flagged for a particular service
	// pushErrors is a map that keeps track of how many *pushErrors have been flagged for a particular service and node.
	// The key to pushErrors should be a string indicating the node we are keeping a count for
	pushErrors map[string]errorCount
}

// setErrorCountsByService queries the db.ManagedTokensDatabase to load the prior errorCounts for a given service
func setErrorCountsByService(ctx context.Context, service string, database *db.ManagedTokensDatabase) (*serviceErrorCounts, bool) {
	// Only track errors if we have a valid ManagedTokensDatabase
	if database == nil {
		return nil, false
	}

	ec := &serviceErrorCounts{}
	tChan := make(chan error, 2)
	var tWg sync.WaitGroup

	// Check for setupError counts
	tWg.Add(1)
	go func() {
		defer tWg.Done()
		var err error
		setupErrorData, err := database.GetSetupErrorsInfoByService(ctx, service)
		defer func() {
			tChan <- err
		}()
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.WithField("service", service).Debug("No setupError information for service.  Assuming there are no prior errors")
				err = nil
			} else {
				log.WithField("service", service).Error("Could not get setupError information. Please inspect database")
			}
			return
		}
		ec.setupErrors.value = setupErrorData.Count()
	}()

	// Check for pushErrorCounts
	tWg.Add(1)
	go func() {
		defer tWg.Done()
		var err error
		ec.pushErrors = make(map[string]errorCount)
		pushErrorData, err := database.GetPushErrorsInfoByService(ctx, service)
		defer func() {
			tChan <- err
		}()
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.WithField("service", service).Debug("No pushError information for service.  Assuming there are no prior errors")
				err = nil
			} else {
				log.WithField("service", service).Error("Could not get pushError information.  Please inspect database")
			}
			return
		}
		for _, datum := range pushErrorData {
			errorCountVal := errorCount{value: datum.Count()}
			ec.pushErrors[datum.Node()] = errorCountVal
		}
	}()

	tWg.Wait() // Wait until we finish sending any errors with regard to getting error count info
	close(tChan)

	// Listen on tChan to see if we got any errors getting error counts from ManagedTokensDatabase
	for err := range tChan {
		if err != nil {
			log.Error("Error getting error info from database.  Will not track errors")
			return nil, false
		}
	}
	return ec, true
}

// setupErrorCount is a type that encapsulates a service and the number of setupErrors registered for that service.  It implements db.SetupErrorCount
// and thus can be used to store setupError counts into the ManagedTokensDatabase
type setupErrorCount struct {
	service string
	count   int
}

func (s *setupErrorCount) Service() string { return s.service }
func (s *setupErrorCount) Count() int      { return s.count }

// pushErrorCount is a type that encapsulates a service, node and the number of pushErrors registered for that service and node.  It implements
// db.PushErrorCount and thus can be used to store pushError counts into the ManagedTokensDatabase
type pushErrorCount struct {
	service string
	node    string
	count   int
}

func (p *pushErrorCount) Service() string { return p.service }
func (p *pushErrorCount) Node() string    { return p.node }
func (p *pushErrorCount) Count() int      { return p.count }

// saveErrorCountsInDatabase stores the serviceErrorCounts into the ManagedTokensDatabase.  It will not store a value that is both zero
// and unchanged as determined by the underlying errorCount objects in the passed in serviceErrorCounts
func saveErrorCountsInDatabase(ctx context.Context, service string, database *db.ManagedTokensDatabase, ec *serviceErrorCounts) error {
	// Save the value to the database for the following cases:
	// 1.  The value has been changed
	// 2.  The value is non-zero, but unchanged.  This means there was no error registered for that errorCount, and thus the underlying
	// issue can be assumed to be fixed.  Reset the value to 0, and store it.
	// Otherwise, don't save the value (only true if the value is 0, and was not changed).
	shouldStoreValue := func(e *errorCount) (bool, int) {
		if e.changed {
			return true, e.value
		}
		if e.value != 0 {
			return true, 0
		}
		return false, 0
	}

	// Setup Errors.  Only do this bit if setupErrors was actually set - not if it's 0, for example, from initialization
	if storeSetupErrorCount, value := shouldStoreValue(&ec.setupErrors); storeSetupErrorCount {
		s := setupErrorCount{service, value}
		if err := database.UpdateSetupErrorsTable(ctx, []db.SetupErrorCount{&s}); err != nil {
			log.WithField("service", service).Error("Could not save new setupError counts in database")
			return err
		}
		log.WithField("service", service).Debug("Updated setupError counts in database")
	}

	// Push Errors
	pushErrorsCountSlice := make([]db.PushErrorCount, 0, len(ec.pushErrors))
	for node, count := range ec.pushErrors {
		if storePushErrorCount, value := shouldStoreValue(&count); storePushErrorCount {
			pushErrorsCountSlice = append(pushErrorsCountSlice, &pushErrorCount{service, node, value})
		}
	}

	if len(pushErrorsCountSlice) != 0 {
		if err := database.UpdatePushErrorsTable(ctx, pushErrorsCountSlice); err != nil {
			log.WithField("service", service).Error("Could not save new pushError counts in database")
			return err
		}
		log.WithField("service", service).Debug("Updated pushError counts in database")
	}
	return nil
}

// adjustErrorCountsByServiceAndDirectNotification takes a Notification, adjusts the applicable errorCount, and based on the current errorCount value
// and the configured minimum threshhold for sending messages, will return whether or not that Notification should be flagged to be sent to the stakeholder
func adjustErrorCountsByServiceAndDirectNotification(n Notification, ec *serviceErrorCounts, errorCountToSendMessage int) (sendNotification bool) {
	adjustCount := func(count int) (newCount int, shouldSendNotification bool) {
		// Increment count
		newCount = count + 1
		if newCount >= errorCountToSendMessage {
			// Reset the counter to 0, allow for notification to be staged for sending
			newCount = 0
			shouldSendNotification = true
			// We're under our threshhold for sending notifications, so sendNotification remains false
		}
		return newCount, shouldSendNotification
	}

	var newValue int
	if nValue, ok := n.(*pushError); ok {
		// Evaluate the pushError count and change it if needed
		if pushErrorCountVal, pushErrorCountOk := ec.pushErrors[nValue.node]; pushErrorCountOk {
			newValue, sendNotification = adjustCount(pushErrorCountVal.value)
			pushErrorCountVal.set(newValue)
			ec.pushErrors[nValue.GetNode()] = pushErrorCountVal
		} else {
			// First time we have an error for this service/node combo. Start the counter, do not send notification
			ec.pushErrors[nValue.GetNode()] = errorCount{1, true}
		}
		log.WithFields(log.Fields{
			"service":          nValue.GetService(),
			"node":             nValue.GetNode(),
			"count":            ec.pushErrors[nValue.GetNode()].value,
			"sendNotification": sendNotification,
		}).Debug("Adjusted count for pushError")
		return
	}
	// For setupErrors, if we're tracking the count, examine the current count and change it as needed
	if _, ok := n.(*setupError); ok {
		newValue, sendNotification = adjustCount(ec.setupErrors.value)
		ec.setupErrors.set(newValue)
		log.WithFields(log.Fields{
			"service":          n.GetService(),
			"count":            ec.setupErrors.value,
			"sendNotification": sendNotification,
		}).Debug("Adjusted count for setupError")
	}
	if !sendNotification {
		log.WithFields(log.Fields{
			"service":             n.GetService(),
			"count":               newValue,
			"notificationMinimum": errorCountToSendMessage,
			"sendNotification":    sendNotification,
		}).Debug("Will not send notification - error count is less than threshhold to send notification.")
	}
	return
}
