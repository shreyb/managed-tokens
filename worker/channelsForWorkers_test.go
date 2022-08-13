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
		description         string
		userInputBufferSize int
		expectedBufferSize  int
	}
	randomBufferSize := rand.Intn(100)

	testCases := []testCase{
		{"User input 0 buffer - should override and set buffer to 1", 0, 1},
		{"User input 1 buffer - should set buffer to 1", 1, 1},
		{"Random buffer size - should be preserved", randomBufferSize, randomBufferSize},
	}

	for _, test := range testCases {
		t.Run(test.description, func(t *testing.T) {
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
		})
	}
}
