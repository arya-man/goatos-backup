package herdstage

import (
	"reflect"
	"testing"
)

// TestFatteningMembershipIsMatchedWholeAndCaseInsensitively pins the vocabulary both folds read:
// the three stored values are one cohort however they are spelled, and a code that merely starts
// with F2 is NOT one of them.
func TestFatteningMembershipIsMatchedWholeAndCaseInsensitively(t *testing.T) {
	for _, stage := range []string{"F2", "F2-Male", "F2-Female", "f2-female", " F2-Male "} {
		if !IsFattening(stage) {
			t.Fatalf("IsFattening(%q) = false, want true", stage)
		}
	}
	// F2-Trial and F2X are the farm's to mean as it likes; a prefix test would claim them.
	for _, stage := range []string{"F2-Trial", "F2X", "K1", "K3", "Buck", "Mother", "ICU", ""} {
		if IsFattening(stage) {
			t.Fatalf("IsFattening(%q) = true, want false", stage)
		}
	}
}

// TestLowerMembersIsTheVocabularyBoundToSQL: the two folds happen in SQL, which compares against
// lower(btrim(...)), so the bound list must be lower-cased and complete. A short list here is a
// silent half-fold, which is worse than none.
func TestLowerMembersIsTheVocabularyBoundToSQL(t *testing.T) {
	got := LowerMembers()
	want := []string{"f2", "f2-male", "f2-female"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LowerMembers() = %v, want %v", got, want)
	}
	for _, member := range got {
		if !IsFattening(member) {
			t.Fatalf("LowerMembers() offered %q, which IsFattening rejects — the two must agree", member)
		}
	}
}

// TestDisplayLabelRenamesOnlyTheFatteningFamily: the lookup names every code, and showing them all
// would turn K3 into "Weaned kids" on screens the farm reads by code.
func TestDisplayLabelRenamesOnlyTheFatteningFamily(t *testing.T) {
	if got := DisplayLabel("F2", "Fattening"); got != "Fattening" {
		t.Fatalf("DisplayLabel(F2) = %q, want the configured name", got)
	}
	if got := DisplayLabel("K3", "Weaned kids"); got != "K3" {
		t.Fatalf("DisplayLabel(K3) = %q, want the code kept", got)
	}
	// No authored name is not an invitation to invent one, or to borrow a member's.
	if got := DisplayLabel("F2", "  "); got != "F2" {
		t.Fatalf("DisplayLabel(F2, blank) = %q, want the code", got)
	}
}

// TestSelectingFatteningMatchesAllThreeStoredValues is the other half: the one option a reader can
// now tick has to reach every animal it claims to count.
func TestSelectingFatteningMatchesAllThreeStoredValues(t *testing.T) {
	got := ExpandFilter([]string{FatteningKey})
	want := []string{"F2", "F2-Male", "F2-Female"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandFilter([F2]) = %v, want %v", got, want)
	}
}

// TestStageFilterExpansionLeavesEveryOtherSelectionAlone guards the no-filter and the
// unrelated-stage cases: a farm that never used the sexed tags must get the query it got before
// this fold existed, key for key.
func TestStageFilterExpansionLeavesEveryOtherSelectionAlone(t *testing.T) {
	if got := ExpandFilter(nil); len(got) != 0 {
		t.Fatalf("ExpandFilter(nil) = %v, want empty so 'no filter' still means no filter", got)
	}
	got := ExpandFilter([]string{"K2", "Mother"})
	if want := []string{"K2", "Mother"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandFilter(%v) = %v, want it unchanged", want, got)
	}
}

// TestASexedStageSentDirectlyIsNotWidened records the deliberate asymmetry. A bookmarked URL or an
// older client can still name `F2-Male`; it means that one stored value, and answering it with the
// whole cohort would hand back animals nobody asked for.
func TestASexedStageSentDirectlyIsNotWidened(t *testing.T) {
	got := ExpandFilter([]string{"F2-Male"})
	if want := []string{"F2-Male"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandFilter([F2-Male]) = %v, want %v", got, want)
	}
}

// TestFatteningExpansionNeverDuplicatesAMember covers the mixed selection an older client can send:
// `F2` beside one of the values it expands to must not bind the same value twice.
func TestFatteningExpansionNeverDuplicatesAMember(t *testing.T) {
	got := ExpandFilter([]string{"F2-Female", FatteningKey, "K3"})
	want := []string{"F2-Female", "F2", "F2-Male", "K3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandFilter = %v, want %v", got, want)
	}
}
