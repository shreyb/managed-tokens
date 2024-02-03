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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetReceiveChan(t *testing.T) {
	s := new(ServiceEmailManager)
	c := make(chan Notification)
	funcOpt := SetReceiveChan(c)
	funcOpt(s)
	assert.Equal(t, c, s.ReceiveChan)
}

func TestSetAdminNotificationManager(t *testing.T) {
	s := new(ServiceEmailManager)
	a := new(AdminNotificationManager)
	funcOpt := SetAdminNotificationManager(a)
	funcOpt(s)
	assert.Equal(t, a, s.AdminNotificationManager)

}

func TestSetServiceEmailManagerNotificationMinimum(t *testing.T) {
	s := new(ServiceEmailManager)
	n := 42
	funcOpt := SetServiceEmailManagerNotificationMinimum(n)
	funcOpt(s)
	assert.Equal(t, n, s.NotificationMinimum)
}
