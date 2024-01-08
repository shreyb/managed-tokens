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

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

// scheddCache is a cache where the schedds corresponding to each collector are stored.  It is a container for a sync.Map,
// which contains a map[string]*scheddCacheEntry, where the key is the collector host
type scheddCache struct {
	cache sync.Map
}

// scheddCacheEntry is an entry that contains a *scheddCollection and a *sync.Once to ensure that it is populated exactly once
type scheddCacheEntry struct {
	*scheddCollection
	once *sync.Once
}

// populateFromCollector queries the condor collector for the schedds and stores them in scheddCacheEntry
func (s *scheddCacheEntry) populateFromCollector(collectorHost, constraint string) error {
	schedds, err := getScheddsFromCondor(collectorHost, constraint)
	if err != nil {
		log.Error("Could not populate schedd Cache Entry from condor")
		return err
	}
	s.scheddCollection.storeSchedds(schedds)
	return nil
}
