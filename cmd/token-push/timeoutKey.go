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

package main

// timeoutKey is a type that represents a key for a timeout value for an operation
type timeoutKey uint8

const (
	timeoutGlobal timeoutKey = iota
	timeoutKerberos
	timeoutVaultStorer
	timeoutPing
	timeoutPush
	invalidTimeoutKey // boundary to assist in validation tests
)

func (t timeoutKey) String() string {
	switch t {
	case timeoutGlobal:
		return "global"
	case timeoutKerberos:
		return "kerberos"
	case timeoutVaultStorer:
		return "vaultstorer"
	case timeoutPing:
		return "ping"
	case timeoutPush:
		return "push"
	default:
		return ""
	}
}

func getTimeoutKeyFromString(s string) (timeoutKey, bool) {
	for i := timeoutKey(0); i < invalidTimeoutKey; i++ {
		if i.String() == s {
			return i, true
		}
	}
	return invalidTimeoutKey, false
}
