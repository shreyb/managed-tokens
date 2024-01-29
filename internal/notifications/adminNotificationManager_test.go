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
	"testing"
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
