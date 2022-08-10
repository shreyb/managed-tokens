package notifications

import "sync"

func syncMapLength(m sync.Map) (length int) {
	m.Range(func(key, value interface{}) bool {
		length++
		return true
	})
	return length
}
