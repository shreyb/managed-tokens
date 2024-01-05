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
	"fmt"
	"strings"
	"testing"

	"github.com/olekukonko/tablewriter"
	"github.com/shreyb/managed-tokens/internal/utils"
)

// TestPrepareTableStringFromMap checks that PrepareTableStringFromMap properly translates a map into a table
func TestPrepareTableStringFromMap(t *testing.T) {
	var b strings.Builder
	// Test Data
	testMap := map[string]string{
		"key 1": "error 1",
		"key 2": "error 2",
		"key 3": "error 3",
	}
	mapTable := PrepareTableStringFromMap(testMap, "", []string{})

	// Expected data
	testSliceSliceString := [][]string{
		{"key 1", "error 1"},
		{"key 2", "error 2"},
		{"key 3", "error 3"},
	}
	sliceSliceStringTable := tablewriter.NewWriter(&b)
	sliceSliceStringTable.AppendBulk(testSliceSliceString)
	sliceSliceStringTable.SetBorder(false)
	sliceSliceStringTable.Render()
	expectedTable := fmt.Sprintf("\n\n%s", b.String())

	// Check to make sure all rows show up in both tables, whichever the order
	expectedTableSlice := strings.Split(expectedTable, "\n")
	mapTableSlice := strings.Split(mapTable, "\n")

	if len(expectedTableSlice) != len(mapTableSlice) {
		t.Error("Expected table and test table are of different sizes")
	}

	if ok := utils.IsSliceSubSlice(expectedTableSlice, mapTableSlice); !ok {
		t.Errorf("Expected line does not appear in test table")
		t.Errorf(
			"Got wrong table string.  Expected %s, got %s",
			expectedTable,
			mapTable,
		)
	}
}
