package worker

import (
	"errors"
	"sync"
	"testing"

	"github.com/shreyb/managed-tokens/internal/service"
	"github.com/shreyb/managed-tokens/internal/testutils"
)

type badFunctionalOptError struct {
	msg string
}

func (b badFunctionalOptError) Error() string {
	return b.msg
}

func functionalOptGood(*Config) error {
	return nil
}

func functionalOptBad(*Config) error {
	return badFunctionalOptError{msg: "Bad functional opt"}
}

// TestNewConfig checks that NewConfig properly applies various functional options when initializing the returned *worker.Config
func TestNewConfig(t *testing.T) {
	type testCase struct {
		description    string
		functionalOpts []func(*Config) error
		expectedError  error
	}
	testCases := []testCase{
		{
			description: "New service.Config with only good functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptGood,
				functionalOptGood,
			},
			expectedError: nil,
		},
		{
			description: "New service.Config with only bad functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptBad,
				functionalOptBad,
			},
			expectedError: badFunctionalOptError{msg: "Bad functional opt"},
		},
		{
			description: "New service.Config with a mix of good and bad functional opts",
			functionalOpts: []func(*Config) error{
				functionalOptGood,
				functionalOptBad,
			},
			expectedError: badFunctionalOptError{msg: "Bad functional opt"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			s := service.NewService("myawesomeservice")
			_, err := NewConfig(s, tc.functionalOpts...)

			// Equality check of errors
			if !errors.Is(err, tc.expectedError) {
				t.Errorf("Errors do not match.  Expected %s, got %s", tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestRegisterUnpingableNode(t *testing.T) {
	type testCase struct {
		helptext       string
		priorNodes     []string
		nodeToRegister string
		expectedNodes  []string
	}

	testCases := []testCase{
		{
			"No prior nodes",
			[]string{},
			"node1",
			[]string{"node1"},
		},
		{
			"Prior nodes - add one more",
			[]string{"node1", "node2"},
			"node3",
			[]string{"node1", "node2", "node3"},
		},
	}

	for _, test := range testCases {
		config := Config{unPingableNodes: &unPingableNodes{sync.Map{}}}
		for _, priorNode := range test.priorNodes {
			config.unPingableNodes.Store(priorNode, struct{}{})
		}

		config.RegisterUnpingableNode(test.nodeToRegister)

		finalNodes := make([]string, 0)
		config.unPingableNodes.Range(func(key, value any) bool {
			if keyVal, ok := key.(string); ok {
				finalNodes = append(finalNodes, keyVal)
			}
			return true
		})

		if !testutils.SlicesHaveSameElements(test.expectedNodes, finalNodes) {
			t.Errorf("Expected registered unpingable nodes is different than results.  Expected %v, got %v", test.expectedNodes, finalNodes)
		}
	}
}

func TestIsNodeUnpingable(t *testing.T) {
	type testCase struct {
		helptext       string
		priorNodes     []string
		nodeToCheck    string
		expectedResult bool
	}

	testCases := []testCase{
		{
			"No prior nodes",
			[]string{},
			"node1",
			false,
		},
		{
			"Prior nodes - check should pass",
			[]string{"node1", "node2"},
			"node1",
			true,
		},
		{
			"Prior nodes - check should fail",
			[]string{"node1", "node2"},
			"node3",
			false,
		},
	}

	for _, test := range testCases {
		config := Config{unPingableNodes: &unPingableNodes{sync.Map{}}}
		for _, priorNode := range test.priorNodes {
			config.unPingableNodes.Store(priorNode, struct{}{})
		}

		if result := config.IsNodeUnpingable(test.nodeToCheck); result != test.expectedResult {
			t.Errorf("Got wrong result for registration check.  Expected %t, got %t", test.expectedResult, result)
		}
	}
}
