package ping

import (
	"context"
	"fmt"
	"slices"
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

func (g goodNode) PingNode(ctx context.Context) error {
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	fmt.Println("Running fake PingNode")
	return nil
}

func (g goodNode) String() string { return string(g) }

type badNode string

func (b badNode) PingNode(ctx context.Context) error {
	time.Sleep(1 * time.Microsecond)
	if e := ctx.Err(); e != nil {
		return e
	}
	return fmt.Errorf("exit status 2 ping: unknown host %s", b)
}

func (b badNode) String() string { return string(b) }

// Tests
// TestPingNodeGood pings a PingNoder and makes sure we get no error
func TestPingNodeGood(t *testing.T) {
	// Control test
	if testing.Verbose() {
		t.Log("Running control test")
	}
	g := Node(goodhost)
	if err := g.PingNode(ctx); err != nil {
		t.Errorf("Expected error to be nil but got %s", err)
		t.Errorf("Our \"reliable\" host, %s, is probably down or just not responding", goodhost)
	}
}

// TestPingNodeBad pings a known bogus PingNoder and makes sure we get the error we expect
func TestPingNodeBad(t *testing.T) {
	if testing.Verbose() {
		t.Log("Running bogus host test")
	}
	b := Node(badhost)
	if err := b.PingNode(ctx); err != nil {
		lowerErr := strings.ToLower(err.Error())
		if !strings.Contains(lowerErr, "unknown host") && !strings.Contains(lowerErr, badhost) {
			t.Errorf("Expected error message containing the phrase \"unknown host\" and %s but got %s", badhost, err)
		}
	} else {
		t.Error("Expected some error.  Didn't get any")
	}
}

// TestPingNodeTimeout pings a known good PingNoder with a 1 ns timeout and makes sure we get the timeout error we expect
func TestPingNodeTimeout(t *testing.T) {
	// Timeout
	if testing.Verbose() {
		t.Log("Running timeout test")
	}
	g := Node(goodhost)
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, time.Duration(1*time.Nanosecond))
	time.Sleep(time.Duration(1 * time.Microsecond))
	if err := g.PingNode(timeoutCtx); err != nil {
		lowerErr := strings.ToLower(err.Error())
		expectedMsg := "context deadline exceeded"
		if lowerErr != expectedMsg {
			t.Errorf("Expected error message to be %s.  Got %s instead", expectedMsg, lowerErr)
		}
	} else {
		t.Error("Expected some timeout error.  Didn't get any")
	}
	cancelTimeout()
}

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
	pingChannel := PingAllNodes(ctx, goodNode(""), badNode(badhost), goodNode(""))
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
	pingChannel := PingAllNodes(timeoutCtx, badNode(""), badNode(badhost))
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

func TestParseAndExecutePingTemplate(t *testing.T) {
	node := "mynode"
	expected := []string{"-W", "5", "-c", "1", node}

	if result, err := parseAndExecutePingTemplate(node); !slices.Equal(result, expected) {
		t.Errorf("Got wrong result.  Expected %v, got %v", expected, result)
	} else if err != nil {
		t.Errorf("Should have gotten nil error.  Got %v instead", err)
	}

}
