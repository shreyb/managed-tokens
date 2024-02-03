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
	"testing"

	"github.com/fermitools/managed-tokens/internal/environment"
	"github.com/stretchr/testify/assert"
)

func TestAllConfigOptions(t *testing.T) {
	c := new(Config)
	type testCase struct {
		description   string
		funcOptSetup  func() ConfigOption // Get us our test ConfigOption to apply to c
		expectedValue any
		testValueFunc func() any // The return value of this func is the value we're testing
	}

	testCases := []testCase{
		{
			"SetCommandEnvironment",
			func() ConfigOption {
				cmdEnvFunc := func(e *environment.CommandEnvironment) { e.SetCondorCreddHost("creddHost") }
				return SetCommandEnvironment(cmdEnvFunc)
			},
			"creddHost",
			func() any { return c.CommandEnvironment.GetValue(environment.CondorCreddHost) },
		},
		{
			"SetUserPrincipal",
			func() ConfigOption {
				userPrincipal := "myuserPRINCIPAL"
				return SetUserPrincipal(userPrincipal)
			},
			"myuserPRINCIPAL",
			func() any { return c.UserPrincipal },
		},
		{
			"SetNodes",
			func() ConfigOption {
				nodes := []string{"node1", "node2"}
				return SetNodes(nodes)
			},
			[]string{"node1", "node2"},
			func() any { return c.Nodes },
		},
		{
			"TestSetAccount",
			func() ConfigOption {
				account := "myaccount"
				return SetAccount(account)
			},
			"myaccount",
			func() any { return c.Account },
		},
		{
			"TestSetKeytabPath",
			func() ConfigOption {
				keytabPath := "/path/to/keytab"
				return SetKeytabPath(keytabPath)
			},
			"/path/to/keytab",
			func() any { return c.KeytabPath },
		},
		{
			"TestSetDesiredUID",
			func() ConfigOption {
				uid := uint32(42)
				return SetDesiredUID(uid)
			},
			uint32(42),
			func() any { return c.DesiredUID },
		},
		{
			"TestSetSchedds",
			func() ConfigOption {
				schedds := []string{"schedd1", "schedd2"}
				return SetSchedds(schedds)
			},
			[]string{"schedd1", "schedd2"},
			func() any { return c.Schedds },
		},
		{
			"TestSetVaultServer",
			func() ConfigOption {
				vaultServer := "vault.server.host"
				return SetVaultServer(vaultServer)
			},
			"vault.server.host",
			func() any { return c.VaultServer },
		},
		{
			"TestSetServiceCreddVaultTokenPathRoot",
			func() ConfigOption {
				root := "/path/to/vault/token/root"
				return SetServiceCreddVaultTokenPathRoot(root)
			},
			"/path/to/vault/token/root",
			func() any { return c.ServiceCreddVaultTokenPathRoot },
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				funcOpt := test.funcOptSetup()
				funcOpt(c)
				assert.Equal(t, test.expectedValue, test.testValueFunc())
			},
		)
	}
}
