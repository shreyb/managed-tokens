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

package cmdUtils

import "sync"

// scheddCollection is a concurrent-safe collection of schedds.  It should be written to only once
// and read from as many times as needed
type scheddCollection struct {
	schedds []string
	mu      sync.RWMutex
}

// newScheddCollection initializes and returns a *scheddCollection for populating
func newScheddCollection() *scheddCollection {
	s := &scheddCollection{}
	s.schedds = make([]string, 0)
	return s
}

// storeSchedds stores the given schedds in the scheddCollection struct.  It is concurrent-safe.
func (s *scheddCollection) storeSchedds(schedds []string) {
	s.mu.Lock()
	s.schedds = append(s.schedds, schedds...)
	s.mu.Unlock()
}

// getSchedds retrieves the stored schedds from the scheddCollection struct. It is concurrent-safe.
func (s *scheddCollection) getSchedds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schedds
}
