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
	"reflect"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
)

// PrepareTableStringFromMap formats a map[string]string and appends a message onto the beginning
func PrepareTableStringFromMap(m map[string]string, helpMessage string, header []string) string {
	if len(m) == 0 {
		return ""
	}
	table := mapStringStringToTable(m, header)
	finalTable := fmt.Sprintf("%s\n\n%s", helpMessage, table)
	return finalTable
}

// mapStringMapStringErrorToTable takes a map[string]map[string]string and generates a table using the provided header slice
func mapStringStringToTable(myMap map[string]string, header []string) string {
	data := wrapMapToTableData(myMap)
	return createBasicTable(data, header)
}

// createBasicTable creates a basic table from data in the form of a [][]string object with a header
func createBasicTable(data [][]string, header []string) string {
	var b strings.Builder
	table := tablewriter.NewWriter(&b)
	table.SetHeader(header)
	table.AppendBulk(data)
	table.SetBorder(false)
	table.Render()
	return b.String()
}

// wrapMapToTableData wraps MapToTable by taking a map, getting its value, and then passing that to MapToTable with the proper initialization parameters.  This or a function like it should be used by external APIs as opposed to MapToTable.
func wrapMapToTableData(myObject any) [][]string {
	defer func() {
		if r := recover(); r != nil {
			log.Panicf("Panicked when generating table data, %s", r)
		}
	}()

	v := reflect.ValueOf(myObject)
	return mapToTableData(
		v,
		[][]string{},
		[]string{},
	)
}

// mapToTableData takes an arbitrarily nested map whose value is given in v, iterates through it, and returns each unique key(s)/value set as a row.  Adapted from https://stackoverflow.com/a/53159340
func mapToTableData(v reflect.Value, curData [][]string, curRow []string) [][]string {
	rowStage := append([]string(nil), curRow...)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.CanInterface() {
			if val, ok := v.Interface().(error); ok {
				rowStage = append(rowStage, val.Error())
				curData = append(curData, rowStage)
				return curData
			}
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		for _, k := range v.MapKeys() {
			rowStage := append(rowStage, k.String())
			curData = mapToTableData(v.MapIndex(k), curData, rowStage)
		}
	case reflect.Array, reflect.Slice:
		// Empty slice in our structure
		if v.Len() == 0 {
			curData = append(curData, curRow)
			return curData
		}

		for i := 0; i < v.Len(); i++ {
			curData = mapToTableData(v.Index(i), curData, rowStage)
		}
	case reflect.String:
		rowStage = append(rowStage, v.String())
		curData = append(curData, rowStage)
		return curData
	case reflect.Int:
		rowStage = append(rowStage, strconv.FormatInt(v.Int(), 10))
		curData = append(curData, rowStage)
		return curData
	case reflect.Float32:
		rowStage = append(rowStage, strconv.FormatFloat(v.Float(), 'f', -1, 32))
		curData = append(curData, rowStage)
		return curData
	case reflect.Float64:
		rowStage = append(rowStage, strconv.FormatFloat(v.Float(), 'f', -1, 64))
		curData = append(curData, rowStage)
		return curData
	case reflect.Bool:
		rowStage = append(rowStage, strconv.FormatBool(v.Bool()))
		curData = append(curData, rowStage)
		return curData
	default:
		if v.CanInterface() {
			if val, ok := v.Interface().(fmt.Stringer); ok {
				rowStage = append(rowStage, val.String())
				curData = append(curData, rowStage)
			}
		} else {
			curData = append(curData, curRow)
		}
	}
	return curData
}

// aggregateServicePushErrors takes a map[string]string of pushErrors sorted by service and creates a table from them
func aggregateServicePushErrors(servicePushErrors map[string]string) string {
	helpText := "The following is a list of nodes on which all vault tokens were not refreshed, and the corresponding roles for those failed token refreshes:"
	header := []string{"Node", "Error"}
	return PrepareTableStringFromMap(servicePushErrors, helpText, header)
}
