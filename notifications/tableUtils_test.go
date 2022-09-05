package notifications

import (
	"fmt"
	"strings"
	"testing"

	"github.com/olekukonko/tablewriter"
	"github.com/shreyb/managed-tokens/utils"
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

	if err := utils.IsSliceSubSlice(expectedTableSlice, mapTableSlice); err != nil {
		t.Errorf("Expected line does not appear in test table")
		t.Errorf(
			"Got wrong table string.  Expected %s, got %s",
			expectedTable,
			mapTable,
		)
	}
}
