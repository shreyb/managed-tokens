package worker

import (
	"math/rand"
	"testing"
)

// type testChanGroup struct {
// 	serviceConfigChan chan *service.Config
// 	successChan chan SuccessReporter
// }

func TestNewChannelsForWorkersDefaultBuffer(t *testing.T) {
	type testCase struct {
		userInputBufferSize int
		expectedBufferSize  int
	}
	randomBufferSize := rand.Intn(100)

	testCases := []testCase{
		{0, 1},
		{1, 1},
		{randomBufferSize, randomBufferSize},
	}

	for _, test := range testCases {
		n := NewChannelsForWorkers(test.userInputBufferSize)
		if _, ok := n.(*channelGroup); !ok {
			t.Errorf("Got wrong return type for NewChannelsForWorkers.  Wanted %T, got %T", &channelGroup{}, n)
		}

		if size := cap(n.GetServiceConfigChan()); size != test.expectedBufferSize {
			t.Errorf("Buffer size for GetServiceConfigChan() should be %d.  Got %d instead", test.expectedBufferSize, size)
		}

		if size := cap(n.GetSuccessChan()); size != test.expectedBufferSize {
			t.Errorf("Buffer size for GetSuccessChan() should be %d.  Got %d instead", test.expectedBufferSize, size)
		}
	}
}
