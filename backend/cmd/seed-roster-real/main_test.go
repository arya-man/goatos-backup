package main

import (
	"strings"
	"testing"
)

func TestValidateStrictRosterRejectsUnresolvedAndMissingOwners(t *testing.T) {
	st := stats{MappingRows: 1, PositionSlotsDefined: 1, AssignmentsUnresolved: 1}
	err := validateStrictRoster(nil, nil, st)
	if err == nil {
		t.Fatal("strict validation accepted an unresolved, ownerless roster")
	}
	for _, want := range []string{"unresolved assignments=1", "missing resolved CBE/preventive_care_manager", "missing resolved CPT/park_head"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("strict error %q missing %q", err, want)
		}
	}
}

func TestValidateStrictRosterAcceptsRequiredCenterOwnership(t *testing.T) {
	var assignments []rosterAssignment
	for _, center := range []string{"CBE", "CPT"} {
		for _, position := range []string{"Preventive Care Manager", "Backup Manager", "Park Head"} {
			assignments = append(assignments, rosterAssignment{
				center: center, position: positionDefs[position], isResolved: true,
			})
		}
	}
	st := stats{MappingRows: len(assignments), PositionSlotsDefined: len(assignments)}
	if err := validateStrictRoster(nil, assignments, st); err != nil {
		t.Fatalf("strict validation rejected complete ownership: %v", err)
	}
}
