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

package notifications

import "sync"

// adminData stores the information needed to generate the admin message
type adminData struct {
	SetupErrors []string
	PushErrors  sync.Map
}

// packageErrors is a concurrent-safe struct that holds information about all the errors encountered while running package funcs and methods
// It also includes a sync.Mutex and a sync.WaitGroup to coordinate data access
type packageErrors struct {
	errorsMap   *sync.Map      // Holds all the errors accumulated for the current invocation.  Roughly a map[service string]*adminData
	writerCount sync.WaitGroup // WaitGroup to be incremented anytime a function wants to write to a packageErrors
	mu          sync.Mutex
}
