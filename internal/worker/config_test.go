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
	"errors"
	"sync"
	"testing"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/fermitools/managed-tokens/internal/service"
	testUtils "github.com/fermitools/managed-tokens/internal/testUtils"
	"github.com/stretchr/testify/assert"
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
		functionalOpts []ConfigOption
		expectedError  error
	}
	testCases := []testCase{
		{
			description: "New service.Config with only good functional opts",
			functionalOpts: []ConfigOption{
				functionalOptGood,
				functionalOptGood,
			},
			expectedError: nil,
		},
		{
			description: "New service.Config with only bad functional opts",
			functionalOpts: []ConfigOption{
				functionalOptBad,
				functionalOptBad,
			},
			expectedError: badFunctionalOptError{msg: "Bad functional opt"},
		},
		{
			description: "New service.Config with a mix of good and bad functional opts",
			functionalOpts: []ConfigOption{
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

func TestBadFunctionalOptPreserveOldState(t *testing.T) {
	badFunctionalOpt := func(c *Config) error {
		c.Account = "badaccount"
		return badFunctionalOptError{"This is an error"}
	}

	s := service.NewService("test_service")
	c, err := NewConfig(s, badFunctionalOpt)

	assert.ErrorIs(t, err, badFunctionalOptError{"This is an error"})
	assert.Equal(t, c.Account, "")
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

		if !testUtils.SlicesHaveSameElementsOrdered[string](test.expectedNodes, finalNodes) {
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

func TestBackupConfig(t *testing.T) {
	s := service.NewService("my_service")
	e := environment.CommandEnvironment{}

	c1 := &Config{
		Service:                        s,
		UserPrincipal:                  "user_principal",
		Nodes:                          []string{"node1", "node2"},
		Account:                        "myaccount",
		KeytabPath:                     "/path/to/keytab",
		ServiceCreddVaultTokenPathRoot: "/path/to/service/credd/vaulttoken/path/root",
		DesiredUID:                     12345,
		Schedds:                        []string{"schedd1", "schedd2"},
		VaultServer:                    "vault.server.host",
		CommandEnvironment:             e,

		Extras: map[supportedExtrasKey]any{DefaultRoleFileDestinationTemplate: "/path/to/template"},
	}
	c1.unPingableNodes = &unPingableNodes{sync.Map{}}
	c1.unPingableNodes.Store("foo", struct{}{})

	c2 := backupConfig(c1)

	assert.Equal(t, c1.Service, c2.Service)
	assert.Equal(t, c1.UserPrincipal, c2.UserPrincipal)
	assert.Equal(t, c1.Nodes, c2.Nodes)
	assert.Equal(t, c1.Account, c2.Account)
	assert.Equal(t, c1.KeytabPath, c2.KeytabPath)
	assert.Equal(t, c1.ServiceCreddVaultTokenPathRoot, c2.ServiceCreddVaultTokenPathRoot)
	assert.Equal(t, c1.DesiredUID, c2.DesiredUID)
	assert.Equal(t, c1.Schedds, c2.Schedds)
	assert.Equal(t, c1.VaultServer, c2.VaultServer)
	assert.Equal(t, c1.CommandEnvironment, c2.CommandEnvironment)

	assert.Equal(t, c1.Extras, c2.Extras)
	assert.Equal(t, c1.unPingableNodes, c2.unPingableNodes)
	assert.Equal(t, c1.extraPingArgs, c2.extraPingArgs)
}
