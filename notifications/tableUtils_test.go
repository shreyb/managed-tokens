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

	if expectedTable != mapTable {
		t.Errorf(
			"Got wrong table string.  Expected %s, got %s",
			expectedTable,
			mapTable,
		)
	}

}
