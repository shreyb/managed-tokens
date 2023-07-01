package cmdUtils

import (
	"testing"

	"github.com/shreyb/managed-tokens/internal/testUtils"
)

func TestStoreSchedds(t *testing.T) {
	expectedSchedds := []string{"schedd1", "schedd2", "schedd3"}
	s := newScheddCollection()
	s.storeSchedds(expectedSchedds)
	if !testUtils.SlicesHaveSameElements(s.schedds, expectedSchedds) {
		t.Errorf("Wrong elements stored.  Expected %v, got %v", expectedSchedds, s.schedds)
	}
}

func TestGetSchedds(t *testing.T) {
	expectedSchedds := []string{"schedd1", "schedd2", "schedd3"}
	s := newScheddCollection()
	s.schedds = append(s.schedds, expectedSchedds...)
	result := s.getSchedds()
	if !testUtils.SlicesHaveSameElements(result, expectedSchedds) {
		t.Errorf("Wrong elements stored.  Expected %v, got %v", expectedSchedds, result)
	}
}
