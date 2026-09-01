package postgres

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// AN UNSUPPORTED ORIGIN IS A BAD REQUEST, NOT A BROKEN SERVER — the same rule, and the same
// reason, as its sibling in sex_scope_test.go. Kept as a pure unit test on purpose: it must run
// in every environment, including the ones where the Postgres suite is opted out.
//
// "all" is in the bad list deliberately. The screen's vocabulary carries its own All, and it
// spells it as an ABSENT parameter rather than the literal string; accepting "all" here would
// give "every kid" two spellings that could drift apart.
func TestNormalizeOriginFilterRejectsUnknownValuesAsInvalidArgument(t *testing.T) {
	for _, bad := range []string{"foo", "born", "bought", "all", "farm", "load", "0"} {
		got, err := normalizeOriginFilter(bad)
		if err == nil {
			t.Fatalf("normalizeOriginFilter(%q) must fail, got %q", bad, got)
		}
		// errors.Is, not a string match: the HTTP mapper switches on this sentinel, so a message
		// that merely READS like a validation error would still answer 500.
		if !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("normalizeOriginFilter(%q) must wrap ports.ErrInvalidArgument so the API answers 400, got %v", bad, err)
		}
	}

	for _, in := range []struct{ raw, want string }{
		{"", ""},
		{"farm_born", OriginFarmBorn},
		{"purchased", OriginPurchased},
		{" PURCHASED ", OriginPurchased},
	} {
		got, err := normalizeOriginFilter(in.raw)
		if err != nil {
			t.Fatalf("normalizeOriginFilter(%q): %v", in.raw, err)
		}
		if got != in.want {
			t.Fatalf("normalizeOriginFilter(%q) = %q, want %q", in.raw, got, in.want)
		}
	}
}

// AN UNSELECTED FILTER WIDENS; A FILTER THAT MATCHED NOTHING NARROWS. The two are told apart by
// the `applied` flag and never by the list being empty, because inferring one from the other is
// how a page ends up showing every kid on screen under a heading naming one cohort.
//
// This is the load-bearing property of composing two cohort filters, and it is easy to get wrong
// in the direction that fails OPEN: a reader picking "Female" and "Purchased" who is shown every
// kid on the farm has no way to tell from the screen that the filter did nothing.
func TestIntersectScopesTreatsUnselectedAndEmptyDifferently(t *testing.T) {
	female := ReportScope{
		Tags:            []string{"tag-a", "tag-b"},
		AllTimeTags:     []string{"tag-a", "tag-b", "tag-old"},
		LocationIDs:     []string{"loc-1", "loc-2"},
		PartitionLabels: []string{"Part 1", ""},
		allTimeResolved: true,
	}
	purchased := ReportScope{
		Tags:            []string{"tag-b", "tag-c"},
		AllTimeTags:     []string{"tag-b", "tag-c", "tag-old"},
		LocationIDs:     []string{"loc-2", "loc-3"},
		PartitionLabels: []string{"", "Part 9"},
		allTimeResolved: true,
	}

	// Neither selected: the scope names nothing, and every read treats that as "no filter".
	if got := IntersectScopes(female, false, purchased, false); !got.Empty() {
		t.Fatalf("no filter selected must yield an empty scope, got %+v", got)
	}

	// One selected: that side passes through untouched. Intersecting against the other side's
	// (unresolved, therefore empty) lists would filter every animal out and report an empty page
	// for a filter the reader did select.
	if got := IntersectScopes(female, true, purchased, false); len(got.Tags) != 2 || len(got.LocationIDs) != 2 {
		t.Fatalf("sex alone must pass through unchanged, got %+v", got)
	}
	if got := IntersectScopes(female, false, purchased, true); len(got.Tags) != 2 || got.Tags[0] != "tag-b" {
		t.Fatalf("origin alone must pass through unchanged, got %+v", got)
	}

	// Both selected: the rows in BOTH.
	both := IntersectScopes(female, true, purchased, true)
	if len(both.Tags) != 1 || both.Tags[0] != "tag-b" {
		t.Fatalf("both filters must intersect the tag lists, got %v", both.Tags)
	}
	if len(both.AllTimeTags) != 2 {
		t.Fatalf("both filters must intersect the all-time tag lists, got %v", both.AllTimeTags)
	}

	// BUCKETS INTERSECT AS PAIRS, NEVER AS TWO INDEPENDENT LISTS. loc-2 appears on both sides, so
	// it survives; the partition beside it must be ITS partition. Intersecting the two arrays
	// separately would keep loc-1/loc-2 from one side beside "Part 1"/"" from the other and name a
	// pen neither filter claimed — the parallel-array defect the arrays are built in one pass to
	// avoid.
	if len(both.LocationIDs) != 1 || both.LocationIDs[0] != "loc-2" {
		t.Fatalf("bucket intersection must keep only the shared pen, got %v", both.LocationIDs)
	}
	if len(both.PartitionLabels) != len(both.LocationIDs) || both.PartitionLabels[0] != "" {
		t.Fatalf("bucket arrays must stay paired by index, got locations %v partitions %v", both.LocationIDs, both.PartitionLabels)
	}

	// A cohort that matched NOTHING must narrow to nothing, not widen to everything.
	none := ReportScope{Tags: []string{}, AllTimeTags: []string{}, LocationIDs: []string{}, PartitionLabels: []string{}}
	if got := IntersectScopes(female, true, none, true); len(got.Tags) != 0 || len(got.LocationIDs) != 0 {
		t.Fatalf("a cohort matching nothing must yield an empty page, got %+v", got)
	}
}

// A PEN IS A (LOCATION, PARTITION) PAIR, AND INTERSECTING THE TWO ARRAYS SEPARATELY INVENTS ONE.
//
// This fixture is built so that the wrong implementation produces a CONFIDENT WRONG ANSWER rather
// than an obviously broken one, which is why the earlier version of this test did not catch it:
// intersecting locations independently yields {godel-2} and partitions independently yields
// {Part 4}, so a separate-array implementation reports the pen "Godel 2 / Part 4" — a pen that
// exists, that the reader can see on screen, and that NEITHER filter selected. The correct answer
// is that the two cohorts share no pen at all.
//
// One physical shed carries several pens and each is weighed on its own, so this is the ordinary
// shape of this farm's data, not a contrived one.
func TestIntersectScopesKeepsPartitionsOfOneShedApart(t *testing.T) {
	a := ReportScope{
		Tags: []string{}, AllTimeTags: []string{},
		LocationIDs:     []string{"godel-2", "castro"},
		PartitionLabels: []string{"Part 1", "Part 4"},
	}
	b := ReportScope{
		Tags: []string{}, AllTimeTags: []string{},
		LocationIDs:     []string{"godel-2"},
		PartitionLabels: []string{"Part 4"},
	}
	got := IntersectScopes(a, true, b, true)
	if len(got.LocationIDs) != 0 || len(got.PartitionLabels) != 0 {
		t.Fatalf("the two cohorts share no pen, so the intersection must be empty; got locations %v partitions %v", got.LocationIDs, got.PartitionLabels)
	}

	// And the genuinely shared pen DOES survive, so the rule above is not passing by refusing
	// everything.
	b.LocationIDs = []string{"godel-2", "castro"}
	b.PartitionLabels = []string{"Part 9", "Part 4"}
	got = IntersectScopes(a, true, b, true)
	if len(got.LocationIDs) != 1 || got.LocationIDs[0] != "castro" || got.PartitionLabels[0] != "Part 4" {
		t.Fatalf("the shared pen must survive intact, got locations %v partitions %v", got.LocationIDs, got.PartitionLabels)
	}
}
