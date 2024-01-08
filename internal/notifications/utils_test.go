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

import (
	"math/rand"
	"sync"
	"testing"
)

// TestSyncMapLength checks that syncMapLength properly measures the length of a sync.Map
func TestSyncMapLength(t *testing.T) {
	var m sync.Map
	length := rand.Intn(100)

	for i := 1; i <= length; i++ {
		m.Store(i, struct{}{})
	}

	if testLength := syncMapLength(&m); testLength != length {
		t.Errorf(
			"Got wrong sync.Map length.  Expected %d, got %d",
			length,
			testLength,
		)
	}

}
