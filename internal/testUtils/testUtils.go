// Package testUtils is for testing utilities used across the codebase

package testUtils

import (
	"cmp"
	"reflect"
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

	bCounter := make(map[C]int)
	for _, bElt := range b {
		bCounter[bElt]++
	}

	return reflect.DeepEqual(aCounter, bCounter)
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
