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
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	// adminErrors holds all the errors to be translated and sent to admins running the various utilities.
	// Callers should increment the writerCount waitgroup upon starting up, and decrement when they return.
	adminErrors packageErrors
)

// adminDataUnsync is an intermediate data structure between *adminData and AdminDataFinal that translates the adminData.PushErrors sync.Map
// to a regular map[string]string
type adminDataUnsync struct {
	SetupErrors []string
	PushErrors  map[string]string
}

// addErrorToAdminErrors takes the passed in Notification, type-checks it, and adds it to the appropriate field of adminErrors
func addErrorToAdminErrors(n Notification) {
	adminErrors.mu.Lock()
	funcLogger := log.WithField("caller", "notifications.addErrorToAdminErrors")
	defer adminErrors.mu.Unlock()

	// The first time addErrorToAdminErrors is called, initialize the errorsMap so we don't get a nil pointer dereference panic
	// later on when we try to check the sync.Map for values
	if adminErrors.errorsMap == nil {
		m := sync.Map{}
		adminErrors.errorsMap = &m
	}

	switch nValue := n.(type) {
	// For *setupErrors, store or append the setupError text to the appropriate field
	case *setupError:
		if data, loaded := adminErrors.errorsMap.LoadOrStore(
			nValue.service,
			&adminData{
				SetupErrors: []string{nValue.message},
			},
		); loaded {
			// Service already has *adminData stored
			if accumulatedAdminData, ok := data.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				// Just append the newest setup error to the slice
				accumulatedAdminData.SetupErrors = append(accumulatedAdminData.SetupErrors, nValue.message)
			}
		}
	// This case is a bit more complicated, since the pushErrors are stored in a sync.Map
	case *pushError:
		data, loaded := adminErrors.errorsMap.LoadOrStore(
			// Roughly an initialization of the PushErrors sync.Map
			nValue.service,
			&adminData{
				PushErrors: sync.Map{},
			},
		)
		if loaded {
			if accumulatedAdminData, ok := data.(*adminData); !ok {
				funcLogger.Panic("Invalid data stored in admin errors map.")
			} else {
				accumulatedAdminData.PushErrors.Store(nValue.node, nValue.message)
			}
		} else {
			// At this point, since we didn't wrap the LoadOrStore call in an if-contraction (if <expression>; loaded {})
			// we know that if loaded == false, then adminErrors.errorsMap[nValue.service] = &adminData{PushErrors: sync.Map{}}
			// from above.  So all we need to do is load the pointer value, type-check it, and store our message.
			//
			// We need to do it this way because otherwise, we'd have to instantiate a sync.Map with the values stored, and then
			// copy it into adminErrors, which copies the underlying mutex.  That could lead to concurrency issues later.
			if accumulatedAdminData, ok := adminErrors.errorsMap.Load(nValue.service); ok {
				if accumulatedAdminDataVal, ok := accumulatedAdminData.(*adminData); ok {
					accumulatedAdminDataVal.PushErrors.Store(nValue.node, nValue.message)
				}
			}
		}
	}
}

// adminErrorsToAdminDataUnsync translates the accumulated adminErrors.errorsMap into a map[string]adminDataUnsync so that
// we have easier access to the structure of the data
func adminErrorsToAdminDataUnsync() map[string]adminDataUnsync {
	funcLogger := log.WithField("caller", "notifications.adminErrorsToAdminDataUnsync")

	adminErrorsMap := make(map[string]*adminData)
	adminErrorsMapUnsync := make(map[string]adminDataUnsync)
	// 1.  Write adminErrors from sync.Map to Map called adminErrorsMap
	adminErrors.errorsMap.Range(func(service, aData any) bool {
		s, ok := service.(string)
		if !ok {
			funcLogger.Panic("Improper key in admin notifications map.")
		}
		a, ok := aData.(*adminData)
		if !ok {
			funcLogger.Panic("Invalid admin data stored for notification")
		}

		if !a.isEmpty() {
			adminErrorsMap[s] = a
		}
		return true
	})

	// 2. Take adminErrorsMap, convert so that values are adminErrorUnsync objects.
	for service, aData := range adminErrorsMap {
		a := adminDataUnsync{
			SetupErrors: aData.SetupErrors,
			PushErrors:  make(map[string]string),
		}
		aData.PushErrors.Range(func(node, err any) bool {
			n, ok := node.(string)
			if !ok {
				funcLogger.Panic("Improper key in push errors map")
			}
			e, ok := err.(string)
			if !ok {
				funcLogger.Panic("Improper error string in push errors map")
			}

			if e != "" {
				a.PushErrors[n] = e
			}
			return true
		})
		adminErrorsMapUnsync[service] = a
	}
	return adminErrorsMapUnsync
}

// isEmpty checks to see if a variable of type adminData has any data
func (a *adminData) isEmpty() bool {
	return ((len(a.SetupErrors) == 0) && (syncMapLength(&a.PushErrors) == 0))
}

// adminErrorsIsEmpty checks to see if there are no adminErrors
func adminErrorsIsEmpty() bool { return syncMapLength(adminErrors.errorsMap) == 0 }
