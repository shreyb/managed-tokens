// Package testUtils is for testing utilities used across the codebase

package testUtils

import "fmt"

// slicesHaveSameElements compares two slices of comparable type to make sure that they have the same elements.  The ordering of those elements
// does not matter
func SlicesHaveSameElements[C comparable](a, b []C) bool {
	if len(a) != len(b) {
		return false
	}
	for _, aElt := range a {
		var found bool
		for _, bElt := range b {
			if aElt == bElt {
				found = true
				break
			}
		}
		if !found {
			fmt.Println(aElt)
			return false
		}
	}
	return true
}
