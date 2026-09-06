package domain

import "testing"

// The transitional mapping is small enough to read, and every one of these
// invariants is a way it could silently misbehave in SQL rather than fail.
func TestStockFamilyMergeIsAFlatFoldOfExactlyTheFourSplitFeeds(t *testing.T) {
	members, families, labels := StockFamilyMergeArrays()
	if len(members) != len(StockFamilyMerge) || len(families) != len(members) || len(labels) != len(members) {
		t.Fatalf("the three arrays are bound as parallel columns and must stay the same length: %d/%d/%d",
			len(members), len(families), len(labels))
	}

	seenMember := map[string]bool{}
	familyLabel := map[string]string{}
	for i, m := range members {
		if seenMember[m] {
			// unnest would emit the member twice, fanning its balance out.
			t.Errorf("member %q mapped twice", m)
		}
		seenMember[m] = true
		if labels[i] == "" {
			t.Errorf("member %q has no family label, so its card would lose its title", m)
		}
		if prev, ok := familyLabel[families[i]]; ok && prev != labels[i] {
			// MAX(family_label) would pick one arbitrarily.
			t.Errorf("family %q carries two labels: %q and %q", families[i], prev, labels[i])
		}
		familyLabel[families[i]] = labels[i]
	}
	for _, f := range families {
		if seenMember[f] {
			// The SQL folds ONE hop (COALESCE(mm.family_key, ...)), so a family
			// that is itself somebody's member would leave a half-folded card.
			t.Errorf("family %q is also a member: the fold is one hop, chains are not supported", f)
		}
	}

	// Every folded feed must still be named by the per-farm Mesha concentrate
	// table. That table is the audit view -- once the cards fold, it is the ONLY
	// place the split is visible -- so a feed folded out of the cards and absent
	// there would disappear from the tab entirely. The table may legitimately
	// name MORE keys than the fold does (it also lists the successors), so this
	// is containment, not equality.
	named := map[string]bool{}
	for _, k := range MeshaConcentrateStockKeys {
		named[k] = true
	}
	for _, m := range members {
		if !named[m] {
			t.Errorf("folded feed %q is not named by MeshaConcentrateStockKeys, so folding it hides it from the tab entirely", m)
		}
	}
}
