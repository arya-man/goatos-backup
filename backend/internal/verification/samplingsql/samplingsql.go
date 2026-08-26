// Package samplingsql holds the ONE SQL definition of "this verification item was drawn for
// review" (maintainer decision 2026-08-26).
//
// It is its own package because TWO modules now count the same business fact. The verifier's queue
// and the leadership KPI strip live in verification; the vaccination live tracker's "videos
// awaiting review" card lives in vaccinationexecution, which already reads verification_items
// directly. AGENTS.md's cross-surface count parity rule is explicit that the same fact must show
// the same number everywhere, so the predicate cannot be written twice -- two copies is how a
// board and a KPI strip come to disagree about how much work is outstanding.
//
// It deliberately exports SQL text rather than a query: each caller keeps its own scope, its own
// indexes and its own projection-review marker. All this owns is the definition of the draw.
package samplingsql

import "fmt"

// DefaultSamplePercent mirrors verification/domain.DefaultSamplePercent. It is restated as a
// literal here rather than imported so this package stays dependency-free at the SQL layer;
// TestDefaultPercentMatchesTheDomain pins the two together.
const DefaultSamplePercent = 100

// BusinessTimezone mirrors platform/biztime.DefaultTimezone, for the same reason.
const BusinessTimezone = "Asia/Kolkata"

// InSample renders the predicate for an item drawn at the share in force on its OWN business day.
//
// alias is the verification_items alias in the caller's query (conventionally "vi"). The
// percentage is resolved PER ITEM rather than per request because one read can span days: a
// backlog page shows yesterday's items under yesterday's share and today's under today's, which is
// the whole reason the policy is effective-dated.
//
// COALESCE to 100: a category the CEO has never set is verified in full, exactly as every queue
// behaved before this feature existed.
func InSample(alias string) string {
	return fmt.Sprintf(`%[1]s.sampling_bucket < COALESCE((
        SELECT p.sample_percent
        FROM verification_sampling_policies p
        WHERE p.tenant_id = %[1]s.tenant_id
          AND p.category = %[1]s.category
          AND p.effective_business_date <= (%[1]s.captured_at AT TIME ZONE '%[2]s')::date
        ORDER BY p.effective_business_date DESC
        LIMIT 1), %[3]d)`, alias, BusinessTimezone, DefaultSamplePercent)
}

// SettledByPolicy names the items the closeout approved because the policy did not draw them.
// Every count of what a HUMAN did excludes them: they carry a verified_at (the closeout's clock)
// and would otherwise read as verdicts nobody cast.
func SettledByPolicy(alias string) string {
	return alias + ".auto_resolution IS NOT NULL"
}

// DecidedByPerson is SettledByPolicy's complement, for the same reads stated the positive way.
func DecidedByPerson(alias string) string {
	return alias + ".auto_resolution IS NULL"
}
