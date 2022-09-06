package notifications

import (
	"math/rand"
	"sync"
	"testing"
)

// TestSyncMapLength checks that syncMapLength properly measures the length of a sync.Map
func TestSyncMapLength(t *testing.T) {
	var m sync.Map
	length := rand.Intn(100)

	for i := 1; i <= length; i++ {
		m.Store(i, struct{}{})
	}

	if testLength := syncMapLength(m); testLength != length {
		t.Errorf(
			"Got wrong sync.Map length.  Expected %d, got %d",
			length,
			testLength,
		)
	}

}
