package main

import (
	"strings"
	"testing"
)

func TestValidateStrictRosterRejectsUnresolvedAndMissingOwners(t *testing.T) {
	mappings := []rosterMappingRow{{center: "CPT", position: "Preventive Care Manager"}}
	st := stats{MappingRows: 1, PositionSlotsDefined: 1, AssignmentsUnresolved: 1}
	err := validateStrictRoster(mappings, nil, st, nil)
	if err == nil {
		t.Fatal("strict validation accepted an unresolved, ownerless roster")
	}
	for _, want := range []string{"unresolved assignments=1", "missing resolved CPT/preventive_care_manager", "missing resolved CPT/park_head"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("strict error %q missing %q", err, want)
		}
	}
}

func TestValidateStrictRosterAcceptsRequiredCenterOwnership(t *testing.T) {
	var mappings []rosterMappingRow
	var assignments []rosterAssignment
	for _, center := range []string{"CBE", "CPT"} {
		for _, position := range []string{"Preventive Care Manager", "Backup Manager", "Park Head"} {
			mappings = append(mappings, rosterMappingRow{center: center, position: position})
			assignments = append(assignments, rosterAssignment{
				center: center, position: positionDefs[position], isResolved: true,
			})
		}
	}
	st := stats{MappingRows: len(assignments), PositionSlotsDefined: len(assignments)}
	if err := validateStrictRoster(mappings, assignments, st, nil); err != nil {
		t.Fatalf("strict validation rejected complete ownership: %v", err)
	}
}

// cptSeatAssignments returns the three resolved jun-26 CPT seats (PC-manager,
// backup-manager, park-head) mapped to Darshan/Sagar/Amit, exactly as the
// CPT operator-drive source CSV resolves before the overlay.
func cptSeatAssignments() []rosterAssignment {
	return []rosterAssignment{
		{center: "CPT", position: positionDefs["Preventive Care Manager"], jun26Name: "Darshan", isResolved: true, weekOffWeekday: "sunday"},
		{center: "CPT", position: positionDefs["Backup Manager"], jun26Name: "Sagar", isResolved: true, weekOffWeekday: "monday"},
		{center: "CPT", position: positionDefs["Park Head"], jun26Name: "Amit", isResolved: true, weekOffWeekday: "tuesday"},
	}
}

func cptContract() *operatorRosterContract {
	c := &operatorRosterContract{}
	c.SourceScope.ParkCode = "CPT"
	defaultCap := 200
	c.OperatorCapacity.DefaultAnimalsPerDay = &defaultCap
	c.Operators = []operatorRosterOperator{
		{Code: "vaccination_operator_amit", DisplayName: "Amit Kumar", Tier: "manager", WeekOff: "friday"},
		{Code: "vaccination_operator_darshan", DisplayName: "Darshan Talwar", Tier: "manager", WeekOff: "sunday"},
		{Code: "vaccination_operator_sagar", DisplayName: "Sagar Mahoor", Tier: "manager", WeekOff: "saturday"},
	}
	return c
}

func TestApplyOperatorRosterOverlayRecastsSeats(t *testing.T) {
	assignments := cptSeatAssignments()
	centers, err := applyOperatorRosterOverlay(cptContract(), assignments)
	if err != nil {
		t.Fatalf("overlay errored on a complete contract: %v", err)
	}
	if !centers["CPT"] {
		t.Fatalf("expected CPT to be a contract-covered center, got %v", centers)
	}
	want := map[string]struct {
		code    string
		weekOff string
	}{
		"Amit":    {"vaccination_operator_amit", "friday"},
		"Darshan": {"vaccination_operator_darshan", "sunday"},
		"Sagar":   {"vaccination_operator_sagar", "saturday"},
	}
	for _, a := range assignments {
		w, ok := want[a.jun26Name]
		if !ok {
			t.Fatalf("unexpected assignment for %q", a.jun26Name)
		}
		if a.position.code != w.code {
			t.Errorf("%s: position code = %q, want %q", a.jun26Name, a.position.code, w.code)
		}
		if a.position.tier != "manager" {
			t.Errorf("%s: tier = %q, want manager", a.jun26Name, a.position.tier)
		}
		if a.position.isBackupSlot {
			t.Errorf("%s: is a backup slot; equal operators must not be backup", a.jun26Name)
		}
		if a.weekOffWeekday != w.weekOff {
			t.Errorf("%s: week-off = %q, want %q", a.jun26Name, a.weekOffWeekday, w.weekOff)
		}
		if a.vaccinationCap == nil || *a.vaccinationCap != 200 {
			t.Errorf("%s: vaccination cap = %v, want 200", a.jun26Name, a.vaccinationCap)
		}
	}
}

func TestApplyOperatorRosterOverlayUsesPerOperatorCap(t *testing.T) {
	assignments := cptSeatAssignments()
	contract := cptContract()
	amitCap := 1
	contract.Operators[0].AnimalCapPerDay = &amitCap

	if _, err := applyOperatorRosterOverlay(contract, assignments); err != nil {
		t.Fatalf("overlay errored on per-operator cap: %v", err)
	}
	for _, a := range assignments {
		if a.jun26Name == "Amit" {
			if a.vaccinationCap == nil || *a.vaccinationCap != 1 {
				t.Fatalf("Amit cap = %v, want per-operator cap 1", a.vaccinationCap)
			}
			continue
		}
		if a.vaccinationCap == nil || *a.vaccinationCap != 200 {
			t.Fatalf("%s cap = %v, want default cap 200", a.jun26Name, a.vaccinationCap)
		}
	}
}

func TestApplyOperatorRosterOverlayErrorsOnUnmatchedOperator(t *testing.T) {
	// Only two of the three declared operators have a resolved seat.
	assignments := cptSeatAssignments()[:2]
	_, err := applyOperatorRosterOverlay(cptContract(), assignments)
	if err == nil {
		t.Fatal("overlay accepted a contract operator with no resolved seat")
	}
	if !strings.Contains(err.Error(), "vaccination_operator_amit") {
		t.Fatalf("error %q should name the unmatched operator vaccination_operator_amit", err)
	}
}

func TestApplyOperatorRosterOverlayNilContractIsNoOp(t *testing.T) {
	assignments := cptSeatAssignments()
	centers, err := applyOperatorRosterOverlay(nil, assignments)
	if err != nil {
		t.Fatalf("nil contract should be a no-op, got %v", err)
	}
	if len(centers) != 0 {
		t.Fatalf("nil contract should cover no centers, got %v", centers)
	}
	if assignments[0].position.code != "preventive_care_manager" {
		t.Fatalf("nil contract must not mutate seats, got %q", assignments[0].position.code)
	}
}

func TestValidateStrictRosterWaivesContractCoveredCenter(t *testing.T) {
	// After the overlay, CPT has only vaccination_operator_* seats and no
	// PC-manager/backup/park-head trio; strict must pass because CPT is a
	// contract-covered center.
	assignments := cptSeatAssignments()
	if _, err := applyOperatorRosterOverlay(cptContract(), assignments); err != nil {
		t.Fatalf("overlay setup failed: %v", err)
	}
	mappings := []rosterMappingRow{
		{center: "CPT", position: "Preventive Care Manager"},
		{center: "CPT", position: "Backup Manager"},
		{center: "CPT", position: "Park Head"},
	}
	st := stats{MappingRows: 3, PositionSlotsDefined: 3}
	if err := validateStrictRoster(mappings, assignments, st, map[string]bool{"CPT": true}); err != nil {
		t.Fatalf("strict validation rejected a contract-covered operator-roster center: %v", err)
	}
}
