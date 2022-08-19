package notifications

import (
	"fmt"
	"strings"
	"testing"

	"github.com/olekukonko/tablewriter"
)

func TestPrepareTableStringFromMap(t *testing.T) {
	var b strings.Builder
	testMap := map[string]string{
		"key 1": "error 1",
		"key 2": "error 2",
		"key 3": "error 3",
	}

	testSliceSliceString := [][]string{
		{"key 1", "error 1"},
		{"key 2", "error 2"},
		{"key 3", "error 3"},
	}

	mapTable := PrepareTableStringFromMap(testMap, "", []string{})
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

	for _, expectedLine := range expectedTableSlice {
		found := false
		for _, testLine := range mapTableSlice {
			if expectedLine == testLine {
				found = true
			}
		}
		if !found {
			t.Errorf("Expected line %s does not appear in test table", expectedLine)
			t.Errorf(
				"Got wrong table string.  Expected %s, got %s",
				expectedTable,
				mapTable,
			)
		}
	}
}
