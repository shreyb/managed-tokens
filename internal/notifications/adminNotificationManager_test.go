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
	"fmt"
	"math/rand"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/fermitools/managed-tokens/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestRequestToCloseReceiveChanContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := new(AdminNotificationManager)

	a.notificationSourceWg.Add(1)
	returned := make(chan struct{})
	go func() {
		a.RequestToCloseReceiveChan(ctx)
		close(returned)
	}()
	cancel()
	select {
	case <-returned:
		return
	case <-a.receiveChan:
		t.Fatal("context cancel should have caused RequestToCloseReceiveChan to return without closing a.ReceiveChan")
	}
}

func TestRequestToCloseReceiveChan(t *testing.T) {
	ctx := context.Background()
	a := new(AdminNotificationManager)
	a.receiveChan = make(chan Notification)

	a.notificationSourceWg.Add(1)
	go a.RequestToCloseReceiveChan(ctx)
	go a.notificationSourceWg.Done()
	select {
	case <-ctx.Done():
		t.Fatal("context should not be canceled and receiveChan should be closed")
	case <-a.receiveChan:
		return
	}
}

func TestRequestToCloseReceiveChanMultiple(t *testing.T) {
	ctx := context.Background()
	a := new(AdminNotificationManager)
	a.receiveChan = make(chan Notification)

	var wg sync.WaitGroup
	defer wg.Wait()
	a.notificationSourceWg.Add(1)
	// We can request to close the channel 10 times, but we should only do it once.  We should not get any panics
	defer func() {
		v := recover()
		if v != nil {
			t.Fatalf("Recovered: %v.  FAIL:  We should not have tried to close the already-closed receiveChan", v)
		}
	}()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.RequestToCloseReceiveChan(ctx)
		}()
	}
	go a.notificationSourceWg.Done()
	select {
	case <-ctx.Done():
		t.Fatal("context should not be canceled and receiveChan should be closed")
	case <-a.receiveChan:
		return
	}
}

// Check that if notificationSourceWg never gets to 0, we don't close receiveChan
func TestRequestToCloseReceiveChanDeadlock(t *testing.T) {
	ctx := context.Background()
	a := new(AdminNotificationManager)
	a.receiveChan = make(chan Notification)

	a.notificationSourceWg.Add(1)
	go a.RequestToCloseReceiveChan(ctx)

	assert.Never(
		t,
		func() bool {
			// This function should never return
			select {
			case <-ctx.Done():
				t.Fatal("context should not be canceled and receiveChan should be closed")
			case <-a.receiveChan:
				return true // This is also a fatal condition, but we let the assert.Never call handle this case
			}
			return true
		}, 2*time.Second, 10*time.Millisecond)
}

func TestRegisterNotificationSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := new(AdminNotificationManager)
	a.receiveChan = make(chan Notification)

	// Listener
	receiveDone := make(chan struct{})
	go func() {
		<-a.receiveChan
		close(receiveDone)
	}()

	// Sender
	sendChan := a.registerNotificationSource(ctx)
	go func() {
		defer close(sendChan)
		sendChan <- NewSetupError("message", "service")
	}()

	select {
	case <-ctx.Done():
		t.Fatal("Registration failed.  Did not receive from a.receiveChan within timeout")
	case <-receiveDone:
		return
	}
}

func TestDetermineIfShouldTrackErrorCounts(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	type testCase struct {
		description             string
		priorTrackErrorCounts   bool
		databaseSetupFunc       func() *db.ManagedTokensDatabase
		expectedShouldTrack     bool
		expectedServicesToTrack []string
	}

	testCases := []testCase{
		{
			"AdminNotificationManager field set to false",
			false,
			func() *db.ManagedTokensDatabase { return nil },
			false,
			nil,
		},
		{
			"nil database",
			true,
			func() *db.ManagedTokensDatabase { return nil },
			false,
			nil,
		},
		{
			"Couldn't open DB",
			true,
			func() *db.ManagedTokensDatabase {
				m, _ := db.OpenOrCreateDatabase(os.DevNull)
				return m
			},
			false,
			nil,
		},
		{
			"No services loaded",
			true,
			func() *db.ManagedTokensDatabase {
				m, _ := db.OpenOrCreateDatabase(path.Join(tmp, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000))))
				return m
			},
			false,
			nil,
		},
		{
			"Good case - services loaded",
			true,
			func() *db.ManagedTokensDatabase {
				m, _ := db.OpenOrCreateDatabase(path.Join(tmp, fmt.Sprintf("managed-tokens-test-%d.db", rand.Intn(10000))))
				m.UpdateServices(ctx, []string{"service1", "service2", "service3"})
				return m
			},
			true,
			[]string{"service1", "service2", "service3"},
		},
	}

	for _, test := range testCases {
		t.Run(test.description, func(t *testing.T) {
			a := new(AdminNotificationManager)
			a.TrackErrorCounts = test.priorTrackErrorCounts
			a.Database = test.databaseSetupFunc()
			if a.Database != nil {
				defer a.Database.Close()
			}
			shouldTrackErrors, servicesToTrack := determineIfShouldTrackErrorCounts(ctx, a)
			assert.Equal(t, test.expectedShouldTrack, shouldTrackErrors)
			assert.Equal(t, test.expectedServicesToTrack, servicesToTrack)

		})
	}
}

// // getAllErrorCountsFromDatabase gets all the current error counts in the *db.ManagedTokensDatabase for
// // every element of services.  If there is an issue doing so, this returns as its second element false, which
// // indicates to the caller not to use the returned map
// func getAllErrorCountsFromDatabase(ctx context.Context, services []string, database *db.ManagedTokensDatabase) (allServiceCounts map[string]*serviceErrorCounts, valid bool) {
// 	funcLogger := log.WithField("caller", "notifications.getAllErrorCountsFromDatabase")
// 	allServiceCounts = make(map[string]*serviceErrorCounts)
// 	for _, service := range services {
// 		ec, err := setErrorCountsByService(ctx, service, database)
// 		if err != nil {
// 			funcLogger.WithField("service", service).Error("Error setting error count.  Will not use error counts")
// 			return nil, false
// 		}
// 		allServiceCounts[service] = ec
// 	}
// 	return allServiceCounts, true
// }

// TODO Test case where we can't get the error count by service.  Maybe give a fake service?

func TestGetAllErrorCountsFromDatabase(t *testing.T) {
	priorSetupErrors := []db.SetupErrorCount{
		&setupErrorCount{
			"service1",
			2,
		},
		&setupErrorCount{
			"service2",
			3,
		},
	}
	priorPushErrors := []db.PushErrorCount{
		&pushErrorCount{
			"service1",
			"node1",
			2,
		},
		&pushErrorCount{
			"service1",
			"node2",
			0,
		},
		&pushErrorCount{
			"service1",
			"node3",
			2,
		},
		&pushErrorCount{
			"service2",
			"node1",
			1,
		},
		&pushErrorCount{
			"service2",
			"node2",
			0,
		},
		&pushErrorCount{
			"service2",
			"node3",
			1,
		},
	}

	expectedServiceCounts := map[string]*serviceErrorCounts{

		"service1": {
			setupErrors: errorCount{2, false},
			pushErrors: map[string]errorCount{
				"node1": {2, false},
				"node2": {0, false},
				"node3": {2, false},
			},
		},
		"service2": {
			setupErrors: errorCount{3, false},
			pushErrors: map[string]errorCount{
				"node1": {1, false},
				"node2": {0, false},
				"node3": {1, false},
			},
		},
	}

	var err error
	a := new(AdminNotificationManager)
	tempDir := t.TempDir()
	services := []string{"service1", "service2"}
	nodes := []string{"node1", "node2", "node3"}
	a.Database, err = createAndPrepareDatabaseForTesting(tempDir, services, nodes, priorSetupErrors, priorPushErrors)
	if err != nil {
		t.Errorf("Error creating and preparing the database: %s", err)
	}
	defer a.Database.Close()

	errorCountsFromDb, ok := getAllErrorCountsFromDatabase(context.Background(), services, a.Database)
	assert.True(t, ok)
	assert.Equal(t, expectedServiceCounts, errorCountsFromDb)

}
