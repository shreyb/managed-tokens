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
