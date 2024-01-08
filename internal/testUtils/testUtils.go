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

// Package testUtils is for testing utilities used across the codebase

package testUtils

import (
	"cmp"
	"slices"
)

// SlicesHaveSameElements compares two slices of comparable type to make sure that they have the same elements.  The ordering of those elements
// does not matter
func SlicesHaveSameElements[C comparable](a, b []C) bool {
	if len(a) != len(b) {
		return false
	}

	aCounter := make(map[C]int)
	for _, aElt := range a {
		aCounter[aElt]++
	}

	// Iterate through b, and every time we find a key, decrement aCounter.
	for _, bElt := range b {
		if _, found := aCounter[bElt]; !found {
			// Value in b wasn't found as key in aCounter - fail
			return false
		} else {
			aCounter[bElt]--
		}
		if aCounter[bElt] == 0 {
			delete(aCounter, bElt)
		}
	}

	return len(aCounter) == 0
}

// SlicesHaveSameElements compares two slices of comparable and cmp.Ordered type to make
// sure that they have the same elements.  The ordering of those elements does not matter.
// This is just a quicker way to test equality of the elements of slices if the types allow.
func SlicesHaveSameElementsOrdered[C interface {
	comparable
	cmp.Ordered
}](a, b []C) bool {
	if len(a) != len(b) {
		return false
	}
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}
