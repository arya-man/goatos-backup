package app

import "testing"

func TestValidateApprovedDriveCombos(t *testing.T) {
	if err := validateApprovedDriveCombos(); err != nil {
		t.Fatalf("approved combos: %v", err)
	}
}

func TestSplitComboMembersMaxThree(t *testing.T) {
	chunks := splitComboMembers([]string{"ET+TT", "PPR", "FMD", "HS"})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 3 || len(chunks[1]) != 1 {
		t.Fatalf("chunk sizes = %#v, want [3,1]", chunks)
	}
}

func TestComboSessionIDRejectsMoreThanThree(t *testing.T) {
	if _, err := comboSessionID([]string{"ET+TT", "PPR", "FMD", "HS"}); err == nil {
		t.Fatal("expected combo session rejection for 4 vaccines")
	}
}
