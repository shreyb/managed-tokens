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

// ServiceEmailManagerOption is a functional option that should be used as an argument to NewServiceEmailManager to set various fields
// of the ServiceEmailManager
// For example:
//
//	 f := func(a *ServiceEmailManager) error {
//		  a.NotificationMinimum = 42
//	   return nil
//	 }
//	 manager := NewServiceEmailManager(context.Background, f)
type ServiceEmailManagerOption func(*ServiceEmailManager) error

// Note that we don't provide a helper func for setting Service and Email, as those should be passed as arguments directly to NewServiceEmailManager

func SetReceiveChan(s *ServiceEmailManager, c chan Notification) ServiceEmailManagerOption {
	return ServiceEmailManagerOption(func(sem *ServiceEmailManager) error {
		sem.ReceiveChan = c
		return nil
	})
}

func SetAdminNotificationManager(s *ServiceEmailManager, a *AdminNotificationManager) ServiceEmailManagerOption {
	return ServiceEmailManagerOption(func(sem *ServiceEmailManager) error {
		sem.AdminNotificationManager = a
		return nil
	})
}

func SetServiceEmailManagerNotificationMinimum(s *ServiceEmailManager, notificationMinimum int) ServiceEmailManagerOption {
	return ServiceEmailManagerOption(func(sem *ServiceEmailManager) error {
		sem.NotificationMinimum = notificationMinimum
		return nil
	})
}
