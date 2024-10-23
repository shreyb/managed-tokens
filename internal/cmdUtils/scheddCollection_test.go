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

package cmdUtils

import (
	"testing"

	"github.com/fermitools/managed-tokens/internal/testUtils"
)

func TestStoreSchedds(t *testing.T) {
	expectedSchedds := []string{"schedd1", "schedd2", "schedd3"}
	s := newScheddCollection()
	s.storeSchedds(expectedSchedds)
	if !testUtils.SlicesHaveSameElementsOrderedType[string](s.schedds, expectedSchedds) {
		t.Errorf("Wrong elements stored.  Expected %v, got %v", expectedSchedds, s.schedds)
	}
}

func TestGetSchedds(t *testing.T) {
	expectedSchedds := []string{"schedd1", "schedd2", "schedd3"}
	s := newScheddCollection()
	s.schedds = append(s.schedds, expectedSchedds...)
	result := s.getSchedds()
	if !testUtils.SlicesHaveSameElementsOrderedType[string](result, expectedSchedds) {
		t.Errorf("Wrong elements stored.  Expected %v, got %v", expectedSchedds, result)
	}
}
