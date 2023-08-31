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
