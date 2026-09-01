package postgres

import "fmt"

// ReportScope is the resolved answer to "which weighing rows belong to the cohort the reader
// asked for", in terms a weighing query can apply without knowing what an animal is.
//
// It is the SHARED shape of every Weights page filter that narrows the herd rather than the
// window: today Sex (sex_scope.go) and Farm born / Purchased (origin_scope.go). Both resolve
// their own question in their own file and hand back this same opaque pair of lists, so the
// reads that consume it — shed_weights.go, growth.go, load_weights.go and the Growth Director
// widgets — name no herd table, know nothing about animals, and did not change when the second
// filter was added. Adding a THIRD cohort filter should mean one more resolver and no edit to a
// single read.
//
// The two halves cover the two kinds of weigh, and they are deliberately different shapes
// because the evidence is different: an individual weigh carries a scanned tag, while a
// whole-shed weigh carries no tag at all and can only be attributed through its pen.
type ReportScope struct {
	// Tags are normalized scanned identifiers (lower(btrim(...))) belonging to the cohort.
	// Bounded by the tags actually weighed in the window plus the 90-day gain lookback, not by
	// the herd — a park with 50,000 animals and 300 weighs yields 300 tags.
	Tags []string
	// AllTimeTags is the same set with NO time bound, for the one read that is deliberately not
	// windowed: sale readiness reports each animal's LATEST-EVER weight, so a kid heavy enough to
	// sell but not weighed this fortnight must still be counted. Filtering that read with Tags
	// above silently redefined its denominator from "every animal in this cohort ever weighed" to
	// "every animal in this cohort weighed recently". Use Tags for a windowed read; use this ONLY
	// where the read itself spans all time, or the two will disagree about who exists.
	AllTimeTags []string
	// allTimeResolved records whether AllTimeTags was actually asked for. It exists so that reading
	// it when it was never resolved is a LOUD failure rather than a silent one: an unresolved list
	// is empty, and an empty tag list filters every animal out, so a caller that forgot to ask
	// would quietly report zero sale-ready kids instead of erroring.
	allTimeResolved bool
	// LocationIDs and PartitionLabels are PARALLEL arrays naming whole-shed buckets belonging to
	// the cohort. Parallel arrays rather than a struct slice because they are passed straight into
	// SQL as two binds and zipped there; they are built in ONE pass so an index can never pair a
	// location with another's partition.
	LocationIDs     []string
	PartitionLabels []string
}

// SexScope is the historical name for this shape, from when Sex was the only cohort filter on
// the page. Kept as an alias so the Growth Director package's signatures — which pass the scope
// through half a dozen widget helpers — did not all have to churn when Origin joined it.
//
// Prefer ReportScope in new code: a value that may now carry a purchased-pen list is not a "sex
// scope", and a name that describes only half of what it holds is how the next reader concludes
// the origin filter cannot possibly be in there.
type SexScope = ReportScope

// Empty reports whether this scope names nothing, which is NOT the same question as "is a filter
// in force". A cohort that matches no animal also resolves to an empty scope, and the two are
// told apart by the separate `filtered` flag every read carries — never by this method. Using
// Empty() as "unfiltered" would show a reader the WHOLE herd under a heading that says otherwise.
func (s ReportScope) Empty() bool {
	return len(s.Tags) == 0 && len(s.LocationIDs) == 0
}

// Intersect narrows one cohort scope by another, for a page that has more than one cohort filter
// selected at once — Sex AND Origin, say. The result is the rows in BOTH, which is what a reader
// picking "Female" and "Purchased" is asking for.
//
// An UNSET filter is carried by `applied`, never inferred from an empty list, because the two
// mean opposite things: a filter nobody selected must widen the result to everything, while a
// filter that matched nothing must narrow it to nothing. Inferring one from the other is how a
// page ends up showing every kid on screen under a heading that names one cohort.
func IntersectScopes(a ReportScope, aApplied bool, b ReportScope, bApplied bool) ReportScope {
	switch {
	case !aApplied && !bApplied:
		return ReportScope{Tags: []string{}, AllTimeTags: []string{}, LocationIDs: []string{}, PartitionLabels: []string{}}
	case !aApplied:
		return b
	case !bApplied:
		return a
	}

	out := ReportScope{
		Tags:            intersectStrings(a.Tags, b.Tags),
		AllTimeTags:     intersectStrings(a.AllTimeTags, b.AllTimeTags),
		LocationIDs:     []string{},
		PartitionLabels: []string{},
		// Only claim the all-time list is resolved when BOTH sides resolved it. One side that never
		// ran its unwindowed arm contributes an EMPTY list, and intersecting against empty yields
		// empty — indistinguishable from "no animal qualifies" unless the flag says otherwise.
		allTimeResolved: a.allTimeResolved && b.allTimeResolved,
	}

	// Buckets are zipped by INDEX, so they must be intersected as pairs rather than as two
	// independent lists. Intersecting LocationIDs and PartitionLabels separately would keep a
	// location from one scope beside a partition from the other and silently name a pen neither
	// side claimed — the parallel-array defect the arrays are built in one pass to avoid.
	inB := make(map[[2]string]struct{}, len(b.LocationIDs))
	for i := range b.LocationIDs {
		inB[[2]string{b.LocationIDs[i], b.PartitionLabels[i]}] = struct{}{}
	}
	for i := range a.LocationIDs {
		key := [2]string{a.LocationIDs[i], a.PartitionLabels[i]}
		if _, ok := inB[key]; ok {
			out.LocationIDs = append(out.LocationIDs, key[0])
			out.PartitionLabels = append(out.PartitionLabels, key[1])
		}
	}
	return out
}

func intersectStrings(a, b []string) []string {
	out := []string{}
	if len(a) == 0 || len(b) == 0 {
		return out
	}
	inB := make(map[string]struct{}, len(b))
	for _, v := range b {
		inB[v] = struct{}{}
	}
	seen := make(map[string]struct{}, len(a))
	for _, v := range a {
		if _, ok := inB[v]; !ok {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// assertBucketArraysAgree is the shared guard both resolvers run before returning: the two bucket
// arrays are aggregated under the same ORDER BY over the same rows, so index i must name one
// bucket in both. Built any other way — one DISTINCT and its partner not, or two differently
// ordered aggregates — every index would silently shift and pair a location with another bucket's
// partition.
func assertBucketArraysAgree(what string, scope ReportScope) error {
	if len(scope.LocationIDs) != len(scope.PartitionLabels) {
		return fmt.Errorf("weighing: %s bucket arrays disagree (%d locations, %d partitions)", what, len(scope.LocationIDs), len(scope.PartitionLabels))
	}
	return nil
}
