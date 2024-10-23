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

package testUtils

import (
	"testing"
)

func TestSlicesHaveSameElementsInt(t *testing.T) {
	type testCase struct {
		description    string
		slice1         []int
		slice2         []int
		expectedResult bool
	}

	testCases := []testCase{
		{
			"equal, already ordered",
			[]int{1, 2, 3},
			[]int{1, 2, 3},
			true,
		},
		{
			"equal, non ordered",
			[]int{1, 2, 3},
			[]int{2, 1, 3},
			true,
		},
		{
			"not equal",
			[]int{1, 2, 3},
			[]int{2, 2, 3},
			false,
		},
		{
			"different lengths",
			[]int{1, 2},
			[]int{2, 2, 3},
			false,
		},
		{
			"try to mess it up",
			[]int{1, 2, 2, 2, 3},
			[]int{2, 1, 3, 3, 3},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := SlicesHaveSameElements(test.slice1, test.slice2)
				if result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %t, got %t", test.expectedResult, result)
				}
			},
		)
	}
}

func TestSlicesHaveSameElementsString(t *testing.T) {
	type testCase struct {
		description    string
		slice1         []string
		slice2         []string
		expectedResult bool
	}

	testCases := []testCase{
		{
			"equal, already ordered",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar", "baz"},
			true,
		},
		{
			"equal, non ordered",
			[]string{"foo", "bar", "baz"},
			[]string{"baz", "foo", "bar"},
			true,
		},
		{
			"not equal",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "foo", "bar"},
			false,
		},
		{
			"different lengths",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "foo"},
			false,
		},
		{
			"try to mess it up",
			[]string{"foo", "bar", "baz", "baz"},
			[]string{"foo", "foo", "bar", "baz"},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := SlicesHaveSameElements(test.slice1, test.slice2)
				if result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %t, got %t", test.expectedResult, result)
				}
			},
		)
	}
}

func TestSlicesHaveSameElementsIntOrdered(t *testing.T) {
	type testCase struct {
		description    string
		slice1         []int
		slice2         []int
		expectedResult bool
	}

	testCases := []testCase{
		{
			"equal, already ordered",
			[]int{1, 2, 3},
			[]int{1, 2, 3},
			true,
		},
		{
			"equal, non ordered",
			[]int{1, 2, 3},
			[]int{2, 1, 3},
			true,
		},
		{
			"not equal",
			[]int{1, 2, 3},
			[]int{2, 2, 3},
			false,
		},
		{
			"different lengths",
			[]int{1, 2},
			[]int{2, 2, 3},
			false,
		},
		{
			"try to mess it up",
			[]int{1, 2, 2, 2, 3},
			[]int{2, 1, 3, 3, 3},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := SlicesHaveSameElementsOrderedType[int](test.slice1, test.slice2)
				if result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %t, got %t", test.expectedResult, result)
				}
			},
		)
	}
}

func TestSlicesHaveSameElementsStringOrdered(t *testing.T) {
	type testCase struct {
		description    string
		slice1         []string
		slice2         []string
		expectedResult bool
	}

	testCases := []testCase{
		{
			"equal, already ordered",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar", "baz"},
			true,
		},
		{
			"equal, non ordered",
			[]string{"foo", "bar", "baz"},
			[]string{"baz", "foo", "bar"},
			true,
		},
		{
			"not equal",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "foo", "bar"},
			false,
		},
		{
			"different lengths",
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "foo"},
			false,
		},
		{
			"try to mess it up",
			[]string{"foo", "bar", "baz", "baz"},
			[]string{"foo", "foo", "bar", "baz"},
			false,
		},
	}

	for _, test := range testCases {
		t.Run(
			test.description,
			func(t *testing.T) {
				result := SlicesHaveSameElementsOrderedType[string](test.slice1, test.slice2)
				if result != test.expectedResult {
					t.Errorf("Got wrong result.  Expected %t, got %t", test.expectedResult, result)
				}
			},
		)
	}
}

// Note that on these examples, the SlicesHaveSameElementsOrderedType func is about 30-50
// times faster than SlicesHaveSameElements as measured by these benchmarks, so the
// Ordered func should be used where possible
func BenchmarkSlicesHaveSameElementsOrderedTypeString(b *testing.B) {
	slice1 := []string{"foo", "bar", "baz"}
	slice2 := []string{"baz", "foo", "bar"}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElementsOrderedType[string](slice1, slice2)
	}
}

func BenchmarkSlicesHaveSameElementsString(b *testing.B) {
	slice1 := []string{"foo", "bar", "baz"}
	slice2 := []string{"baz", "foo", "bar"}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElements(slice1, slice2)
	}
}
func BenchmarkSlicesHaveSameElementsOrderedTypeInt(b *testing.B) {
	slice1 := []int{1, 2, 3}
	slice2 := []int{2, 1, 3}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElementsOrderedType[int](slice1, slice2)
	}
}

func BenchmarkSlicesHaveSameElementsInt(b *testing.B) {
	slice1 := []int{1, 2, 3}
	slice2 := []int{2, 1, 3}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElements(slice1, slice2)
	}
}

// Interestingly, the Ordered version is SLOWER when we set up tests that should return false immediately.  Maybe because of the overhead
// of the extra type checks for the Ordered version

func BenchmarkSlicesHaveSameElementsIntFail(b *testing.B) {
	slice1 := []int{1, 2, 3, 3}
	slice2 := []int{2, 1, 3}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElements(slice1, slice2)
	}
}

func BenchmarkSlicesHaveSameElementsOrderedTypeIntFail(b *testing.B) {
	slice1 := []int{1, 2, 3, 3}
	slice2 := []int{2, 1, 3}

	// Run the slice tester on strings
	for n := 0; n < b.N; n++ {
		SlicesHaveSameElementsOrderedType(slice1, slice2)
	}
}
