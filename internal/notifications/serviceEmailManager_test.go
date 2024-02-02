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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackupServiceEmailManager(t *testing.T) {
	s := new(ServiceEmailManager)
	s.Service = "test_service"
	s.Email = NewEmail("from_test", []string{"to1", "to2"}, "test_subject", "smtpHost", 12345)
	s.AdminNotificationManager = new(AdminNotificationManager)
	s.AdminNotificationManager.TrackErrorCounts = true // Note that this is a misconfiguration, but it's just there to make sure we carry the value in the backup copy
	s.NotificationMinimum = 42
	s.wg = &sync.WaitGroup{}
	s.trackErrorCounts = true
	s.errorCounts = &serviceErrorCounts{setupErrors: errorCount{4, true}}
	testBackupServiceEmailManager(t, s)
}

func TestBackupServiceEmailManagerNilPointers(t *testing.T) {
	s := new(ServiceEmailManager)
	testBackupServiceEmailManager(t, s)
}

func testBackupServiceEmailManager(t *testing.T, s1 *ServiceEmailManager) {
	s2 := backupServiceEmailManager(s1)

	assert.Equal(t, s1.Service, s2.Service)
	assert.Equal(t, s1.Email, s2.Email)
	assert.Equal(t, s1.AdminNotificationManager, s2.AdminNotificationManager)
	assert.Equal(t, s1.NotificationMinimum, s2.NotificationMinimum)
	assert.Equal(t, s1.wg, s2.wg)
	assert.Equal(t, s1.trackErrorCounts, s2.trackErrorCounts)
	assert.Equal(t, s1.errorCounts, s2.errorCounts)

	assert.NotNil(t, s2.adminNotificationChan)

	// Check that we get a valid new ReceiveChan that can actually receive
	go func() {
		s2.ReceiveChan <- &setupError{"this is a test message", "test_service"}
		close(s2.ReceiveChan)
	}()
	assert.Eventually(t, func() bool {
		chanVal := <-s2.ReceiveChan
		return assert.Equal(t, "this is a test message", chanVal.GetMessage())
	}, 10*time.Second, 10*time.Millisecond)
}

func TestNewServiceEmailManagerDefault(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	service := "my_service"
	e := NewEmail("from_address", []string{"to_address"}, "test_subject", "smtp.host", 12345)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	s := NewServiceEmailManager(ctx, &wg, service, e)
	newServiceEmailManagerTests(t, s)

}

func TestNewServiceEmailManagerFuncOpt(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	service := "my_service"
	e := NewEmail("from_address", []string{"to_address"}, "test_subject", "smtp.host", 12345)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	s := NewServiceEmailManager(ctx, &wg, service, e,
		ServiceEmailManagerOption(func(sem *ServiceEmailManager) error {
			sem.NotificationMinimum = 42
			return nil
		},
		))
	newServiceEmailManagerTests(t, s, func(t *testing.T, sem *ServiceEmailManager) { assert.Equal(t, 42, sem.NotificationMinimum) })
}

func TestNewServiceEmailManagerFuncOptError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	service := "my_service"
	e := NewEmail("from_address", []string{"to_address"}, "test_subject", "smtp.host", 12345)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })

	s := NewServiceEmailManager(ctx, &wg, service, e,
		ServiceEmailManagerOption(func(sem *ServiceEmailManager) error {
			sem.NotificationMinimum = 42
			return errors.New("This is an error")
		},
		))
	newServiceEmailManagerTests(t, s,
		func(t *testing.T, sem *ServiceEmailManager) {
			t.Run("Test that the effect of our funcOpt got rolled back", func(t *testing.T) {
				assert.Equal(t, 0, sem.NotificationMinimum)
			})
		},
		func(t *testing.T, sem *ServiceEmailManager) {
			t.Run("Test that our backed up ServiceEmailManager is valid", func(t *testing.T) {
				newServiceEmailManagerTests(t, sem)
			})
		},
	)
}

func newServiceEmailManagerTests(t *testing.T, s *ServiceEmailManager, extraTests ...func(*testing.T, *ServiceEmailManager)) {
	assert.Equal(t, "my_service", s.Service)
	assert.NotNil(t, s.ReceiveChan)
	assert.NotNil(t, s.Email)
	assert.NotNil(t, s.AdminNotificationManager)
	assert.NotNil(t, s.adminNotificationChan)
	assert.NotNil(t, s.wg)
	assert.False(t, s.trackErrorCounts)
	assert.Nil(t, s.errorCounts)

	for _, extraTest := range extraTests {
		extraTest(t, s)
	}
}

/* Tests needed:
2. runServiceNotificationHandler
3.addPushErrorNotificationToServiceErrorsTable
4. sendServiceEmailIfErrors with mocked email?
5. prepareServiceEmail
*/
