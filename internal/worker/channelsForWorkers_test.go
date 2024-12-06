// COPYRIGHT 2024 FERMI NATIONAL ACCELERATOR LABORATORY
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package worker

import (
	"math/rand"
	"testing"
)

// TestNewChannelsForWorkers tests that given different buffer sizes, NewChannelsForWorkers returns a ChannelsForWorkers with all channels
// having the correct buffer size
func TestNewChannelsForWorkers(t *testing.T) {
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
			if size := cap(n.GetServiceConfigChan()); size != test.expectedBufferSize {
				t.Errorf("Buffer size for GetServiceConfigChan() should be %d.  Got %d instead", test.expectedBufferSize, size)
			}

			if size := cap(n.GetSuccessChan()); size != test.expectedBufferSize {
				t.Errorf("Buffer size for GetSuccessChan() should be %d.  Got %d instead", test.expectedBufferSize, size)
			}
		})
	}
}
