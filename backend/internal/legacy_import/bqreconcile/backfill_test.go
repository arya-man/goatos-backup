package bqreconcile

import "testing"

func candidate(source, farm, scope, oldTag, breed, gender, status string) Candidate {
	return Candidate{
		Source:   source,
		Farm:     farm,
		ScopeKey: scope,
		OldTag:   oldTag,
		Breed:    breed,
		Gender:   gender,
		Status:   status,
	}
}

func planByOldTag(rows []BackfillPlanRow, oldTag string) (BackfillPlanRow, bool) {
	for _, r := range rows {
		if r.OldTag == oldTag {
			return r, true
		}
	}
	return BackfillPlanRow{}, false
}

func TestLifecycleFromCandidateStatus(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"Active":   {"alive", true},
		"active":   {"alive", true},
		"Sold":     {"sold", true},
		"Inactive": {"inactive", true},
		"dead":     {"", false},
		"weird":    {"", false},
		"":         {"", false},
	}
	for in, want := range cases {
		got, ok := lifecycleFromCandidateStatus(in)
		if got != want.want || ok != want.ok {
			t.Errorf("lifecycleFromCandidateStatus(%q) = (%q,%v), want (%q,%v)", in, got, ok, want.want, want.ok)
		}
	}
}

func TestPlanBackfillDecisions(t *testing.T) {
	candidates := []Candidate{
		candidate("bq_current_latest", "CBE", "park:CBE", "1", "Malai", "Male", "Active"),
		candidate("bq_current_latest", "CPT", "park:CPT", "101", "Malai", "Female", "Sold"),
		candidate("census", "CPT", "park:CPT", "999", "Beetal", "Male", "Inactive"),
		candidate("bq_current_latest", "CBE", "park:CBE", "200", "Malai", "Male", "Active"),  // already present
		candidate("bq_current_latest", "CBE", "wrong:CBE", "300", "Malai", "Male", "Active"), // scope mismatch
		candidate("bq_current_latest", "CBE", "park:CBE", "", "Malai", "Male", "Active"),     // missing identity
		candidate("bq_current_latest", "CBE", "park:CBE", "400", "Malai", "Male", "Dead"),    // unsupported status
	}
	for i := range candidates {
		candidates[i].RowNumber = i + 2
	}
	existing := map[string]struct{}{
		"park:CBE|200": {},
	}

	rows := planBackfill(candidates, existing)
	if len(rows) != len(candidates) {
		t.Fatalf("rows = %d, want %d", len(rows), len(candidates))
	}

	expect := map[string]struct {
		action     string
		skipReason string
		lifecycle  string
	}{
		"1":   {"create", "", "alive"},
		"101": {"create", "", "sold"},
		"999": {"create", "", "inactive"},
		"200": {"skip", "already_present", ""},
		"300": {"skip", "scope_mismatch", ""},
		"":    {"skip", "missing_identity", ""},
		"400": {"skip", "unsupported_status", ""},
	}
	for oldTag, want := range expect {
		row, ok := planByOldTag(rows, oldTag)
		if !ok {
			t.Fatalf("no plan row for old_tag %q", oldTag)
		}
		if row.Action != want.action || row.SkipReason != want.skipReason || row.Lifecycle != want.lifecycle {
			t.Errorf("old_tag %q => (action=%q reason=%q lifecycle=%q), want (action=%q reason=%q lifecycle=%q)",
				oldTag, row.Action, row.SkipReason, row.Lifecycle, want.action, want.skipReason, want.lifecycle)
		}
	}
}

func TestPlanBackfillDuplicateHandling(t *testing.T) {
	// identical duplicates: one create, the rest skipped as duplicate_in_csv
	identical := []Candidate{
		candidate("a", "CBE", "park:CBE", "10", "Malai", "Male", "Active"),
		candidate("b", "CBE", "park:CBE", "10", "Malai", "Male", "Active"),
	}
	for i := range identical {
		identical[i].RowNumber = i + 2
	}
	rows := planBackfill(identical, map[string]struct{}{})
	creates, dups := 0, 0
	for _, r := range rows {
		if r.Action == "create" {
			creates++
		}
		if r.SkipReason == "duplicate_in_csv" {
			dups++
		}
	}
	if creates != 1 || dups != 1 {
		t.Fatalf("identical dup: creates=%d dups=%d, want 1/1", creates, dups)
	}

	// conflicting duplicates: none created, both ambiguous_duplicate
	conflicting := []Candidate{
		candidate("a", "CBE", "park:CBE", "20", "Malai", "Male", "Active"),
		candidate("b", "CBE", "park:CBE", "20", "Beetal", "Female", "Sold"),
	}
	for i := range conflicting {
		conflicting[i].RowNumber = i + 2
	}
	rows = planBackfill(conflicting, map[string]struct{}{})
	for _, r := range rows {
		if r.Action != "skip" || r.SkipReason != "ambiguous_duplicate" {
			t.Errorf("conflicting dup old_tag %q => action=%q reason=%q, want skip/ambiguous_duplicate", r.OldTag, r.Action, r.SkipReason)
		}
	}
}

func TestAttachLocationsUnknownParkFailsClosed(t *testing.T) {
	rows := []BackfillPlanRow{
		{OldTag: "1", Farm: "CBE", Action: "create", Lifecycle: "alive", IdentityState: "clean"},
		{OldTag: "2", Farm: "ZZZ", Action: "create", Lifecycle: "alive", IdentityState: "clean"},
		{OldTag: "3", Farm: "CBE", Action: "create", Lifecycle: "alive", IdentityState: "clean", LastShed: "Godel 2 - Part 3"},
	}
	parks := map[string]string{"CBE": "park-cbe-id"}
	sheds := map[string]LocationTarget{"CBE:GODEL 2 - PART 3": {LocationID: "shed-id", ParentLocationID: "park-cbe-id"}}
	attachLocations(rows, parks, sheds)

	if rows[0].Action != "create" || rows[0].ParkID != "park-cbe-id" || rows[0].CurrentLocID != "park-cbe-id" {
		t.Errorf("park-only create wrong: action=%q park=%q current=%q", rows[0].Action, rows[0].ParkID, rows[0].CurrentLocID)
	}
	if rows[1].Action != "skip" || rows[1].SkipReason != "unknown_park" || rows[1].Lifecycle != "" {
		t.Errorf("unknown park must fail closed: action=%q reason=%q lifecycle=%q", rows[1].Action, rows[1].SkipReason, rows[1].Lifecycle)
	}
	if rows[2].Action != "create" || rows[2].ShedID != "shed-id" || rows[2].ParkID != "park-cbe-id" || rows[2].CurrentLocID != "shed-id" {
		t.Errorf("shed-resolved create wrong: action=%q shed=%q park=%q current=%q", rows[2].Action, rows[2].ShedID, rows[2].ParkID, rows[2].CurrentLocID)
	}
}

func TestPlanBackfillIdempotentReplay(t *testing.T) {
	candidates := []Candidate{
		candidate("bq", "CBE", "park:CBE", "1", "Malai", "Male", "Active"),
		candidate("bq", "CPT", "park:CPT", "2", "Beetal", "Female", "Sold"),
	}
	for i := range candidates {
		candidates[i].RowNumber = i + 2
	}

	first := planBackfill(candidates, map[string]struct{}{})
	existing := map[string]struct{}{}
	for _, r := range first {
		if r.Action == "create" {
			existing[r.ScopeKey+"|"+r.NormalizedValue] = struct{}{}
		}
	}
	if len(existing) != 2 {
		t.Fatalf("first run creates = %d, want 2", len(existing))
	}

	// Second run with those identities now present must plan zero creates.
	second := planBackfill(candidates, existing)
	for _, r := range second {
		if r.Action == "create" {
			t.Errorf("replay planned a create for old_tag %q; want all already_present", r.OldTag)
		}
		if r.Action == "skip" && r.SkipReason != "already_present" {
			t.Errorf("replay old_tag %q skip reason = %q, want already_present", r.OldTag, r.SkipReason)
		}
	}
}
