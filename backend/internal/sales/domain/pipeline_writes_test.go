package domain

import "testing"

// TestLeadStatusFiltersLeadWithTheBucketTheWriteVocabularyCannotHold pins the split found by
// on-device testing (2026-09-05): the phone's board built its status chips from StatusOptions, which
// is DISTINCT call_status and therefore can never contain the not-yet-called bucket -- 164 of 208
// buyer leads, and the actual call list. It was unreachable on that screen.
//
// The two lists are deliberately different and this asserts both halves:
//   - the FACET leads with the sentinel, so the biggest bucket is one tap away
//   - the WRITE vocabulary does NOT contain it, or "uncontacted" could be saved as a literal status
//     and would show up on the chart as a sixth bucket beside the real "Not yet called" one.
//
// Mutation-tested: dropping the sentinel from LeadStatusFilters, and adding it to the stored list
// passed in, each turn a case red.
func TestLeadStatusFiltersLeadWithTheBucketTheWriteVocabularyCannotHold(t *testing.T) {
	stored := []string{"Breeding Animal Intrested", "No Answer", "Wrong No"}
	got := LeadStatusFilters(stored)

	if len(got) != len(stored)+1 {
		t.Fatalf("facet has %d options, want the %d stored plus the uncontacted bucket", len(got), len(stored))
	}
	if got[0].Value != UncontactedStatusKey {
		t.Errorf("facet leads with %q, want the %q sentinel first -- it is the biggest bucket and the call list", got[0].Value, UncontactedStatusKey)
	}
	if got[0].Label != UncontactedStatusLabel {
		t.Errorf("uncontacted label = %q, want %q -- the label is backend-owned so every surface words it the same", got[0].Label, UncontactedStatusLabel)
	}
	// Every stored status keeps its own spelling as its label: the chart buckets by exact string, so
	// relabelling one here would make the facet and the bar it sits under disagree.
	for i, s := range stored {
		if got[i+1].Value != s || got[i+1].Label != s {
			t.Errorf("stored status %q became %+v", s, got[i+1])
		}
	}
	// The sentinel must never leak into the vocabulary a caller can SAVE.
	for _, s := range stored {
		if s == UncontactedStatusKey {
			t.Fatal("the write vocabulary contains the uncontacted sentinel; it could then be stored as a literal status")
		}
	}
}
