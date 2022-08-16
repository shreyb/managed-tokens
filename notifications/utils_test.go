package notifications

import (
	"sync"
	"testing"
)

func TestSyncMapLength(t *testing.T) {
	var m sync.Map
	length := 10

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
