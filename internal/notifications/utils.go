package notifications

import "sync"

// syncMapLength returns the length (number of keys) in a sync.Map
func syncMapLength(m *sync.Map) (length int) {
	if m == nil {
		return 0
	}
	m.Range(func(key, value interface{}) bool {
		length++
		return true
	})
	return length
}
