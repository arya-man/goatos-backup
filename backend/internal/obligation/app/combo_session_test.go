package app

import "testing"

func TestValidateApprovedDriveCombos(t *testing.T) {
	if err := validateApprovedDriveCombos(); err != nil {
		t.Fatalf("approved combos: %v", err)
	}
}

func TestSplitComboMembersMaxTwo(t *testing.T) {
	chunks := splitComboMembers([]string{"ET+TT", "PPR", "FMD"})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 2 || len(chunks[1]) != 1 {
		t.Fatalf("chunk sizes = %#v, want [2,1]", chunks)
	}
}

func TestComboSessionIDRejectsMoreThanTwo(t *testing.T) {
	if _, err := comboSessionID([]string{"ET+TT", "PPR", "FMD"}); err == nil {
		t.Fatal("expected combo session rejection for 3 vaccines")
	}
}
