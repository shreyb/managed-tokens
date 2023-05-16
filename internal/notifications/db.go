package notifications

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/shreyb/managed-tokens/internal/db"
	log "github.com/sirupsen/logrus"
)

// TODO Document this
func setErrorCountsByService(ctx context.Context, service string, database *db.ManagedTokensDatabase) (*serviceErrorCounts, bool) {
	// Only track errors if we have a valid ManagedTokensDatabase
	if database == nil {
		return nil, false
	}

	ec := &serviceErrorCounts{}
	tChan := make(chan error)
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
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("service", service).Error("Could not get setupError information. Please inspect database")
			return
		}
		ec.setupErrors = setupErrorData.Count()
	}()

	// Check for pushErrorCounts
	tWg.Add(1)
	go func() {
		defer tWg.Done()
		ec.pushErrors = make(map[string]int)
		pushErrorData, err := database.GetPushErrorsInfoByService(ctx, service)
		defer func() {
			tChan <- err
		}()
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("service", service).Error("Could not get pushError information.  Please inspect database")
			return
		}
		for _, datum := range pushErrorData {
			ec.pushErrors[datum.Node()] = datum.Count()
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
	// Setup Errors
	s := &setupErrorCount{service, ec.setupErrors}
	if err := database.UpdateSetupErrorsTable(ctx, []db.SetupErrorCount{s}); err != nil {
		log.WithField("service", service).Error("Could not save new setupError counts in database")
		return err
	}
	// Push Errors
	pushErrorsCountSlice := make([]db.PushErrorCount, 0, len(ec.pushErrors))
	for node, count := range ec.pushErrors {
		pushErrorsCountSlice = append(pushErrorsCountSlice, &pushErrorCount{service, node, count})
	}

	if err := database.UpdatePushErrorsTable(ctx, pushErrorsCountSlice); err != nil {
		log.WithField("service", service).Error("Could not save new pushError counts in database")
		return err
	}
	return nil
}

// TODO Document this.  Bool indicates if we should send a message or not
func adjustErrorCountsByServiceAndDirectNotification(n Notification, ec *serviceErrorCounts, notificationMinimum int) (sendNotification bool) {
	adjustCount := func(count int) (newCount int, shouldSendNotification bool) {
		// If our count is less than the minimum, increment the count and don't send a message
		if count < notificationMinimum {
			// We're under our threshhold for sending notifications, so sendNotification remains false
			newCount = count + 1
		} else {
			// Else, reset the counter to 0, allow for notification to be staged for sending
			count = 0
			sendNotification = true
		}
		return newCount, shouldSendNotification
	}

	if nValue, ok := n.(*pushError); ok {
		// Evaluate the pushError count and change it if needed
		if pushErrorCountVal, pushErrorCountOk := ec.pushErrors[nValue.node]; pushErrorCountOk {
			ec.pushErrors[nValue.node], sendNotification = adjustCount(pushErrorCountVal)
		} else {
			// First time we have an error for this service/node combo. Start the counter, do not send notification
			ec.pushErrors[nValue.node] = 1
		}
		return
	}
	// For setupErrors, if we're tracking the count, examine the current count and change it as needed
	if _, ok := n.(*setupError); ok {
		ec.setupErrors, sendNotification = adjustCount(ec.setupErrors)
	}
	return
}
