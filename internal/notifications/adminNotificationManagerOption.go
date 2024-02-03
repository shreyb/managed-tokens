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

import "github.com/fermitools/managed-tokens/internal/db"

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

// These are the defined AdminNotificationManagerOptions that should be used as the arguments to NewAdminNotificationManager

func SetAdminNotificationManagerDatabase(a *AdminNotificationManager, database *db.ManagedTokensDatabase) AdminNotificationManagerOption {
	return AdminNotificationManagerOption(func(anm *AdminNotificationManager) error {
		a.Database = database
		return nil
	})
}

func SetAdminNotificationManagerNotificationMinimum(a *AdminNotificationManager, notificationMinimum int) AdminNotificationManagerOption {
	return AdminNotificationManagerOption(func(anm *AdminNotificationManager) error {
		a.NotificationMinimum = notificationMinimum
		return nil
	})
}

func SetTrackErrorCountsToTrue(a *AdminNotificationManager) AdminNotificationManagerOption {
	return AdminNotificationManagerOption(func(anm *AdminNotificationManager) error {
		a.TrackErrorCounts = true
		return nil
	})
}

func SetDatabaseReadOnlyToTrue(a *AdminNotificationManager) AdminNotificationManagerOption {
	return AdminNotificationManagerOption(func(anm *AdminNotificationManager) error {
		a.DatabaseReadOnly = true
		return nil
	})
}
