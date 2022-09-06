package notifications

import "sync"

// syncMapLength returns the length (number of keys) in a sync.Map
func syncMapLength(m sync.Map) (length int) {
	m.Range(func(key, value interface{}) bool {
		length++
		return true
	})
	return length
}
