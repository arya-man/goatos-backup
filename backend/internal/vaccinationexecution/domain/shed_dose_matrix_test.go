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
