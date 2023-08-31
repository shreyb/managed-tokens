package cmdUtils

import "sync"

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
func (s *scheddCacheEntry) populateFromCollector(collectorHost, constraint string) {
	schedds := getScheddsFromCondor(collectorHost, constraint)
	s.scheddCollection.storeSchedds(schedds)
}
