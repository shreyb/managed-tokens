package notifications

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/shreyb/managed-tokens/internal/db"
	log "github.com/sirupsen/logrus"
)

type errorCount struct {
	value   int
	changed bool
}

func (ec *errorCount) set(val int) {
	ec.value = val
	ec.changed = true
}

type serviceErrorCounts struct {
	setupErrors errorCount
	pushErrors  map[string]errorCount
}

// TODO Document this
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

type setupErrorCount struct {
	service string
	count   int
}

func (s *setupErrorCount) Service() string { return s.service }
func (s *setupErrorCount) Count() int      { return s.count }

type pushErrorCount struct {
	service string
	node    string
	count   int
}

func (p *pushErrorCount) Service() string { return p.service }
func (p *pushErrorCount) Node() string    { return p.node }
func (p *pushErrorCount) Count() int      { return p.count }

// TODO Document this
func saveErrorCountsInDatabase(ctx context.Context, service string, database *db.ManagedTokensDatabase, ec *serviceErrorCounts) error {
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

// TODO Document this.  Bool indicates if we should send a message or not
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

	if nValue, ok := n.(*pushError); ok {
		// Evaluate the pushError count and change it if needed
		if pushErrorCountVal, pushErrorCountOk := ec.pushErrors[nValue.node]; pushErrorCountOk {
			var newValue int
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
			"count":            ec.pushErrors[nValue.GetNode()],
			"sendNotification": sendNotification,
		}).Debug("Adjusted count for pushError")
		return
	}
	// For setupErrors, if we're tracking the count, examine the current count and change it as needed
	if _, ok := n.(*setupError); ok {
		var newValue int
		newValue, sendNotification = adjustCount(ec.setupErrors.value)
		ec.setupErrors.set(newValue)
		log.WithFields(log.Fields{
			"service":          n.GetService(),
			"count":            ec.setupErrors.value,
			"sendNotification": sendNotification,
		}).Debug("Adjusted count for setupError")
	}
	return
}
