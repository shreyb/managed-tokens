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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	goodhost string = "localhost"
	badhost  string = "thisisafakehostandwillneverbeusedever.example.com"
)

var ctx context.Context = context.Background()

type goodNode string

func (g goodNode) PingNode(ctx context.Context, extraPingOpts []string) error {
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	fmt.Println("Running fake PingNode")
	return nil
}

func (g goodNode) String() string { return string(g) }

type badNode string

func (b badNode) PingNode(ctx context.Context, extraPingOpts []string) error {
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	return fmt.Errorf("exit status 2 ping: unknown host %s", b)
}

func (b badNode) String() string { return string(b) }

// TestPingAllNodes makes sure that PingAllNodes returns the errors we expect based on valid/invalid input
// We also make sure that the number of successes and failures reported matches what we expect
func TestPingAllNodes(t *testing.T) {
	var successCount, failureCount int
	numGood := 2
	numBad := 1
	ctx := context.Background()
	if testing.Verbose() {
		t.Logf("Ping all nodes - %d good, %d bad", numGood, numBad)
	}
	extraPingOpts := make([]string, 0)
	pingChannel := pingAllNodes(ctx, extraPingOpts, goodNode(""), badNode(badhost), goodNode(""))
	for n := range pingChannel {
		if n.Err != nil {
			failureCount++
		} else {
			successCount++
		}
	}
	if successCount != numGood || failureCount != numBad {
		t.Errorf("Expected %d good, %d bad nodes.  Got %d good, %d bad instead.", numGood, numBad, successCount, failureCount)
	}
}

// TestPingALlNodesTimeout pings a series of nodes with a 1 ns timeout and makes sure we get the timeout error we expect
func TestPingAllNodesTimeout(t *testing.T) {
	// Timeout
	if testing.Verbose() {
		t.Log("Running timeout test")
	}
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, time.Duration(1*time.Nanosecond))
	extraArgs := make([]string, 0)
	pingChannel := pingAllNodes(timeoutCtx, extraArgs, badNode(""), badNode(badhost))
	for n := range pingChannel {
		if n.Err != nil {
			lowerErr := strings.ToLower(n.Err.Error())
			expectedMsg := "context deadline exceeded"
			if lowerErr != expectedMsg {
				t.Errorf("Expected error message to be %s.  Got %s instead", expectedMsg, lowerErr)
			}
		} else {
			t.Error("Expected some timeout error.  Didn't get any")
		}
	}
	cancelTimeout()
}
