package domain

import "testing"

// TestShedDoseMatrixFlattenResolvesIdentity pins the interning contract: the wire carries indexes,
// and expanding them must reproduce exactly the identity the board would have rendered flat.
func TestShedDoseMatrixFlattenResolvesIdentity(t *testing.T) {
	m := ShedDoseMatrix{
		Sheds: []ShedDoseMatrixShed{
			{ShedID: "s1", ShedName: "Castro", PartitionLabel: "Pen 2", LocationDisplay: "Castro - Pen 2"},
			{ShedID: "s2", ShedName: "Godel"},
		},
		DoseRules: []string{"ET+TT · Dose 1", "Blue Tongue · Dose 1"},
		Cells: []ShedDoseMatrixCell{
			{Shed: 1, Dose: 0, State: "overdue", AnimalCount: 3, MinDueDate: "2026-08-01", MaxDueDate: "2026-08-04"},
			{Shed: 0, Dose: 1, State: "verified", AnimalCount: 7, MinAdministeredDate: "2026-07-30"},
		},
	}

	got := m.Flatten()
	if len(got) != 2 {
		t.Fatalf("Flatten() returned %d cells, want 2", len(got))
	}
	if got[0].ShedID != "s2" || got[0].ShedName != "Godel" || got[0].DoseRule != "ET+TT · Dose 1" {
		t.Fatalf("first cell resolved to the wrong identity: %+v", got[0])
	}
	if got[0].AnimalCount != 3 || got[0].MinDueDate != "2026-08-01" || got[0].MaxDueDate != "2026-08-04" {
		t.Fatalf("first cell lost its measures: %+v", got[0])
	}
	if got[1].LocationDisplay != "Castro - Pen 2" || got[1].PartitionLabel != "Pen 2" {
		t.Fatalf("partition identity did not survive interning: %+v", got[1])
	}
	if got[1].DoseRule != "Blue Tongue · Dose 1" {
		t.Fatalf("dose rule resolved to the wrong label: %+v", got[1])
	}
}

// TestShedDoseMatrixFlattenDropsUnresolvableCells is the safety half of the contract. An index that
// does not resolve is a construction bug, and the board must drop that cell rather than render one
// shed's animal count under another shed's name.
func TestShedDoseMatrixFlattenDropsUnresolvableCells(t *testing.T) {
	m := ShedDoseMatrix{
		Sheds:     []ShedDoseMatrixShed{{ShedID: "s1", ShedName: "Castro"}},
		DoseRules: []string{"ET+TT · Dose 1"},
		Cells: []ShedDoseMatrixCell{
			{Shed: 0, Dose: 0, State: "overdue", AnimalCount: 1},
			{Shed: 9, Dose: 0, State: "overdue", AnimalCount: 500},
			{Shed: 0, Dose: 9, State: "overdue", AnimalCount: 500},
			{Shed: -1, Dose: 0, State: "overdue", AnimalCount: 500},
		},
	}

	got := m.Flatten()
	if len(got) != 1 {
		t.Fatalf("Flatten() kept %d cells, want only the resolvable one: %+v", len(got), got)
	}
	if got[0].AnimalCount != 1 {
		t.Fatalf("Flatten() kept the wrong cell: %+v", got[0])
	}
}

// TestShedDoseMatrixKeepsTwoDoseCodesThatShareALabel is the regression for a defect introduced by
// interning and caught in review.
//
// DoseQualifiedDisplayLabel only qualifies _W1/_W2/_BOOSTER/_REVAC/_REPEAT/_FIRST, so et_tt_kid_4w
// and et_tt_kid_7w BOTH render "ET+TT". Interning the dose axis on that LABEL merged the two codes
// onto one index, so a shed holding both emitted two cells sharing a (shed, dose, state) triple and
// any grid keyed on that triple silently dropped one animal count. The axis is interned on the raw
// dose_code instead, which means DoseRules legitimately holds the same label twice -- consumers
// must key on the INDEX. admin-web's expandShedDoseMatrix/buildShedGrid mirror this with doseKey.
func TestShedDoseMatrixKeepsTwoDoseCodesThatShareALabel(t *testing.T) {
	m := ShedDoseMatrix{
		Sheds:     []ShedDoseMatrixShed{{ShedID: "s1", ShedName: "Castro"}},
		DoseRules: []string{"ET+TT", "ET+TT"},
		Cells: []ShedDoseMatrixCell{
			{Shed: 0, Dose: 0, State: "overdue", AnimalCount: 4},
			{Shed: 0, Dose: 1, State: "overdue", AnimalCount: 9},
		},
	}

	got := m.Flatten()
	if len(got) != 2 {
		t.Fatalf("Flatten() returned %d cells, want 2: two dose codes sharing a label are two doses", len(got))
	}
	total := got[0].AnimalCount + got[1].AnimalCount
	if total != 13 {
		t.Fatalf("animal counts = %d, want 13; a shared label must not collapse two doses", total)
	}
	if got[0].DoseRule != "ET+TT" || got[1].DoseRule != "ET+TT" {
		t.Fatalf("both cells should carry the shared label: %+v / %+v", got[0], got[1])
	}
}
