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

func TestSetCommandEnvironment(t *testing.T) {
	c := new(Config)
	cmdEnvFunc := func(e *environment.CommandEnvironment) { e.SetCondorCreddHost("creddHost") }
	funcOpt := SetCommandEnvironment(cmdEnvFunc)
	funcOpt(c)
	assert.Equal(t, "creddHost", c.CommandEnvironment.GetValue(environment.CondorCreddHost))
}

// // SetUserPrincipal returns a func(*Config) with the UserPrincipal field set to the passed in value
func TestSetUserPrincipal(t *testing.T) {
	c := new(Config)
	userPrincipal := "myuserPRINCIPAL"
	funcOpt := SetUserPrincipal(userPrincipal)
	funcOpt(c)
	assert.Equal(t, userPrincipal, c.UserPrincipal)
}

func TestSetNodes(t *testing.T) {
	c := new(Config)
	nodes := []string{"node1", "node2"}
	funcOpt := SetNodes(nodes)
	funcOpt(c)
	assert.Equal(t, nodes, c.Nodes)
}

func TestSetAccount(t *testing.T) {
	c := new(Config)
	account := "myaccount"
	funcOpt := SetAccount(account)
	funcOpt(c)
	assert.Equal(t, account, c.Account)
}

func TestSetKeytabPath(t *testing.T) {
	c := new(Config)
	keytabPath := "/path/to/keytab"
	funcOpt := SetKeytabPath(keytabPath)
	funcOpt(c)
	assert.Equal(t, keytabPath, c.KeytabPath)

}

func TestSetDesiredUID(t *testing.T) {
	c := new(Config)
	uid := uint32(42)
	funcOpt := SetDesiredUID(uid)
	funcOpt(c)
	assert.Equal(t, uid, c.DesiredUID)
}

func TestSetSchedds(t *testing.T) {
	c := new(Config)
	schedds := []string{"schedd1", "schedd2"}
	funcOpt := SetSchedds(schedds)
	funcOpt(c)
	assert.Equal(t, schedds, c.Schedds)
}

func TestSetVaultServer(t *testing.T) {
	c := new(Config)
	vaultServer := "vault.server.host"
	funcOpt := SetVaultServer(vaultServer)
	funcOpt(c)
	assert.Equal(t, vaultServer, c.VaultServer)
}

func TestSetServiceCreddVaultTokenPathRoot(t *testing.T) {
	c := new(Config)
	root := "/path/to/vault/token/root"
	funcOpt := SetServiceCreddVaultTokenPathRoot(root)
	funcOpt(c)
	assert.Equal(t, root, c.ServiceCreddVaultTokenPathRoot)
}
