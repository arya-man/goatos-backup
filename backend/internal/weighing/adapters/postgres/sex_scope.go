package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Sex scoping for the Weights REPORTING screen.
//
// HERD JOIN BY RECORDED EXCEPTION (maintainer decision 2026-08-26). This is the SECOND
// weighing file permitted to resolve a scanned tag to an animal, after
// weight_demographics.go, and the guard exempts it BY NAME. It exists because the maintainer
// asked for the Weights page's Sex filter to govern the WHOLE page — the shed table, the KPI
// row, the growth leaderboard and the Growth Director widgets — and a weighing row only knows
// a scanned string, so something has to say which strings belong to a male kid.
//
// WHY ONE FILE AND NOT A JOIN IN EACH READ. The alternative was to let shed_weights.go,
// growth.go and growth_director.go each join goat_identifiers. That is exactly the leak the
// 2026-08-04 defect was about, and it would put herd SQL in four more weighing files. Instead
// this file answers the question ONCE and hands the other reads an OPAQUE list: a set of tag
// strings and a set of (location, partition) buckets. Those files still name no herd table,
// still know nothing about animals, and would keep working unchanged if the herd register
// vanished — they would simply be handed an empty scope.
//
// WHAT KEEPS IT SAFE, and what a future change must preserve:
//   - It is READ-ONLY and REPORTING-ONLY. No capture, submit, close or verdict path calls it.
//   - NO scan is gated on identity. This narrows what a REPORT counts; free-flow capture is
//     untouched, and a tag that resolves to nothing is still recorded and still counted in
//     the unfiltered view — it simply cannot answer a question about sex.
//   - An empty sex returns an EMPTY scope, and every caller reads that as "no filter" rather
//     than "nothing matches". The unfiltered page therefore runs the exact query it ran
//     before this file existed.
//
// Widening this exemption — another file, another table, or ANY write path — is a MAINTAINER
// decision, never a developer convenience.

// sexScope is the resolved answer to "which weighing rows belong to this sex", in terms a
// weighing query can apply without knowing what an animal is.
//
// The two halves cover the two kinds of weigh, and they are deliberately different shapes
// because the evidence is different: an individual weigh carries a scanned tag that resolves
// to one animal, while a whole-shed weigh carries no tag at all and can only be attributed
// through the cohort its shed holds.
// SexScope is exported because the Growth Director read lives in its own package and must apply
// the SAME rule from the SAME implementation. Two copies of "which kids are male" would drift,
// and one of the two would be the one a reader is looking at.
type SexScope struct {
	// Tags are normalized scanned identifiers (lower(btrim(...))) whose animal carries the
	// requested sex. Bounded by the tags actually weighed in the window plus the 90-day gain
	// lookback, not by the herd — a park with 50,000 animals and 300 weighs yields 300 tags.
	Tags []string
	// AllTimeTags is the same set with NO time bound, for the one read that is deliberately not
	// windowed: sale readiness reports each animal's LATEST-EVER weight, so a kid heavy enough to
	// sell but not weighed this fortnight must still be counted. Filtering that read with Tags
	// above silently redefined its denominator from "every animal of this sex ever weighed" to
	// "every animal of this sex weighed recently". Use Tags for a windowed read; use this ONLY
	// where the read itself spans all time, or the two will disagree about who exists.
	AllTimeTags []string
	// allTimeResolved records whether AllTimeTags was actually asked for. It exists so that reading
	// it when it was never resolved is a LOUD failure rather than a silent one: an unresolved list
	// is empty, and an empty tag list filters every animal out, so a caller that forgot to ask
	// would quietly report zero sale-ready kids instead of erroring.
	allTimeResolved bool
	// LocationIDs and PartitionLabels are PARALLEL arrays naming whole-shed buckets whose
	// resident cohort is entirely the requested sex. Parallel arrays rather than a struct
	// slice because they are passed straight into SQL as two binds and zipped there; they are
	// built in ONE pass so an index can never pair a location with another's partition.
	LocationIDs     []string
	PartitionLabels []string
}

// empty reports whether no filter is in force, which is the shape every caller checks before
// applying a predicate. Distinguishing it from "resolved to nothing" is the point: a sex that
// matches no animal returns a scope that is NOT empty and correctly yields an empty page.
func (s SexScope) Empty() bool {
	return len(s.Tags) == 0 && len(s.LocationIDs) == 0
}

// normalizeSexFilter accepts the two values the herd register carries and rejects everything
// else, rather than passing an arbitrary string into a predicate. An unknown value is an
// error, never a silent "no filter": silently widening a filter shows a reader more kids than
// they asked for under a heading that says otherwise.
func normalizeSexFilter(sex string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(sex)) {
	case "":
		return "", nil
	case "male":
		return "male", nil
	case "female":
		return "female", nil
	default:
		// WRAPPED IN ErrInvalidArgument so the HTTP layer answers 400, not 500. A bare fmt.Errorf
		// fell through every errors.Is arm of the weighing error mapper and landed on the
		// internal-error path: `?sex=foo` returned "500 internal error" while the handler's own
		// comment claimed unknown values were rejected as invalid input. A caller who mistypes a
		// filter has made a bad REQUEST, and telling them the server broke sends them looking in
		// the wrong place.
		return "", fmt.Errorf("%w: unsupported sex filter %q", ports.ErrInvalidArgument, sex)
	}
}

// resolveSexScope resolves the filter for one window and park scope.
//
// An empty sex short-circuits WITHOUT touching the herd register at all, so the unfiltered
// page — which is every page load until a reader picks a side — pays nothing for this file
// existing and reads no goat row.
//
// projection-review: membership=one row per DISTINCT normalized tag weighed in the window (plus the 90-day gain lookback) that resolves to an animal of this sex, and one row per scoped lump-sum bucket whose residents are all this sex; group_key=the normalized tag, and (location_id, partition_label) for buckets; join_cardinality=ident 0..1 per tag because DISTINCT ON collapses re-issued identifier rows to the newest, goats 1 per goat_id (PK), residents 0..N COLLAPSED by the HAVING aggregate; pagination=NONE, bounded by tags weighed in the window and by scoped buckets; scope=tenant_id + park_id = ANY($2) + the window.
//
// Ratio key sets: none — this returns membership, not a ratio. The HAVING count(DISTINCT sex)
// = 1 is a cap check over the SAME grouped row set the min(sex) is taken from, so a bucket is
// claimed only when its one cohort sex is provably the requested one.
func (r *Repository) resolveSexScope(ctx context.Context, tenantID string, parkIDs []string, sex string, periodStart, periodEnd time.Time) (SexScope, error) {
	return ResolveSexScope(ctx, r.pool, tenantID, parkIDs, sex, periodStart, periodEnd)
}

// resolveSexScopeWithAllTime additionally resolves AllTimeTags, for a read that spans all time.
func (r *Repository) resolveSexScopeWithAllTime(ctx context.Context, tenantID string, parkIDs []string, sex string, periodStart, periodEnd time.Time) (SexScope, error) {
	return resolveSexScope(ctx, r.pool, tenantID, parkIDs, sex, periodStart, periodEnd, true)
}

// ResolveSexScope is the ONE implementation of the rule, callable with any pool so the Growth
// Director read in its own package resolves the same kids the rest of the page does.
// ResolveSexScope resolves the WINDOW-BOUNDED scope: the tags weighed in the selected window plus
// the gain lookback, and the whole-shed buckets in scope. AllTimeTags is deliberately NOT resolved,
// because the all-history scan behind it is pure cost to a caller that never reads it -- which is
// every windowed read on the page. Use ResolveSexScopeWithAllTime when the read spans all time.
func ResolveSexScope(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, sex string, periodStart, periodEnd time.Time) (SexScope, error) {
	return resolveSexScope(ctx, pool, tenantID, parkIDs, sex, periodStart, periodEnd, false)
}

// ResolveSexScopeWithAllTime resolves the same scope PLUS AllTimeTags, for a caller with a read
// that is deliberately not windowed (today: sale readiness, which reports latest-EVER weights).
func ResolveSexScopeWithAllTime(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, sex string, periodStart, periodEnd time.Time) (SexScope, error) {
	return resolveSexScope(ctx, pool, tenantID, parkIDs, sex, periodStart, periodEnd, true)
}

func resolveSexScope(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, sex string, periodStart, periodEnd time.Time, includeAllTime bool) (SexScope, error) {
	out := SexScope{Tags: []string{}, AllTimeTags: []string{}, LocationIDs: []string{}, PartitionLabels: []string{}}
	normalized, err := normalizeSexFilter(sex)
	if err != nil {
		return SexScope{}, err
	}
	if normalized == "" || len(parkIDs) == 0 {
		return out, nil
	}

	const q = ` -- scale-guard:ignore: bounded by the selected window and park list; one reporting read per Weights page load, resolving only the tags actually weighed and the lump-sum buckets actually scoped. The one unwindowed arm (weighed_ever) is OPT-IN behind $6 and executes as a One-Time Filter for every caller that does not ask, so a windowed read pays nothing for it; the single caller that does ask, sale readiness, scans no more than it already scans for itself
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
),
-- The 90-day lookback matches the gain reads: a kid's PREVIOUS weigh can sit before the
-- selected window, and dropping its tag here would silently remove that kid's daily gain from
-- a filtered page while leaving it on the unfiltered one.
weighed AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= ($3::timestamptz - interval '90 days') AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
),
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag, gi.goat_id
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
),
sexed_tags AS (
  SELECT w.tag
  FROM weighed w
  JOIN ident i ON i.tag = w.tag
  JOIN goats g ON g.goat_id = i.goat_id AND g.tenant_id = $1::uuid
  WHERE lower(btrim(g.sex)) = $5::text
),
-- THE SAME TAGS, WITH NO TIME BOUND, for the one read that is deliberately not windowed.
--
-- Sale readiness reports each animal's LATEST-EVER weight on purpose: a kid heavy enough to sell
-- but not weighed this fortnight is still heavy enough to sell, and bounding it to the window
-- would hide exactly the animal the question is about. Narrowing THAT read with the windowed tag
-- list above silently redefined its denominator from "every animal of this sex ever weighed" to
-- "every animal of this sex weighed recently" -- a real regression, and one this farm's data
-- cannot show today because every weigh in it falls inside the 90-day lookback.
--
-- So the window-bounded list stays the default for every windowed read, and this second list
-- serves the unwindowed one. Resolved in the SAME query rather than a second round trip, and it
-- scans no more rows than sale readiness already scans for itself.
weighed_ever AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  -- OPT-IN, and $6 is what keeps it free for everyone else. This arm has no date bound, so it is
  -- an all-history scan of the tenant's weighs -- pure cost to the windowed reads (shed weights,
  -- Growth Director) that share this resolver and never read the result. With $6 false Postgres
  -- resolves this to a One-Time Filter and executes no scan at all (verified by EXPLAIN ANALYZE:
  -- "One-Time Filter: false", actual rows=0), so the cost lands only on the caller that asked.
  WHERE $6::bool
    AND o.tenant_id = $1::uuid
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
),
sexed_tags_ever AS (
  SELECT w.tag
  FROM weighed_ever w
  JOIN ident i ON i.tag = w.tag
  JOIN goats g ON g.goat_id = i.goat_id AND g.tenant_id = $1::uuid
  WHERE lower(btrim(g.sex)) = $5::text
),
-- A whole-shed weigh has no tags, so it is attributed by the cohort its shed holds. The bucket
-- points at a PARTITION (Castro 1) but the herd register puts the animals on the physical shed
-- (Castro), so each bucket resolves to its partition's own residents when it has any and
-- otherwise to its parent shed's — the same resolution the demographics read uses, for the same
-- reason: looking only at the partition finds nothing and silently drops every whole-shed weigh.
shed_targets AS (
  SELECT DISTINCT s.location_id, s.partition_label,
         COALESCE(
           CASE WHEN EXISTS (SELECT 1 FROM goats gg WHERE gg.tenant_id = $1::uuid
                              AND gg.lifecycle_status = 'alive' AND gg.shed_id = s.location_id)
                THEN s.location_id END,
           (SELECT phys.location_id FROM locations phys
            JOIN locations l ON l.location_id = s.location_id AND l.tenant_id = $1::uuid
            WHERE phys.tenant_id = l.tenant_id
              AND phys.parent_location_id = l.parent_location_id
              AND phys.location_type = 'shed'
              AND phys.name = regexp_replace(l.name, '\s*(-\s*)?(Part\s*)?[0-9]+$', '')
            LIMIT 1)
         ) AS resolved_id,
         COALESCE(NULLIF(s.partition_label, ''),
                  NULLIF((regexp_match((SELECT l.name FROM locations l WHERE l.location_id = s.location_id),
                                       '\s*(?:-\s*)?(?:Part\s*)?([0-9]+)$'))[1], ''),
                  '') AS resolved_partition_label
  FROM scoped s
  WHERE s.weighing_category = 'per_shed_partition'
),
-- HAVING count(DISTINCT sex) = 1 is what keeps this honest. The maintainer's rule is that a
-- lump-sum shed holds one sex (2026-08-26), and this claims the bucket only when the register
-- AGREES: a shed that turns out to hold both is claimed by NEITHER side rather than guessed at,
-- because splitting one shed average across a mix invents a distribution nobody measured.
sexed_buckets AS (
  SELECT src.location_id, src.partition_label
  FROM shed_targets src
  JOIN goats g ON g.shed_id = src.resolved_id AND g.tenant_id = $1::uuid
   AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  -- MATCH ON THE SCRUBBED KEY, NOT THE LABEL. The bucket's location is named "Godel 2 - Part 1",
  -- from which the partition extracts as "1", while the register writes the human label
  -- "Part 1" on the goat. Comparing those two strings raw matches NOTHING, so a real shed of 38
  -- males was claimed by neither sex and 76 kids fell out of the page's totals — male + female
  -- stopped adding up to every kid, with nothing on screen to say why. Both sides are reduced to
  -- the same key here: lowercased, trimmed, and with a leading "part" dropped.
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label
  HAVING count(DISTINCT lower(btrim(g.sex))) = 1
     AND min(lower(btrim(g.sex))) = $5::text
)
SELECT
  (SELECT COALESCE(array_agg(tag), '{}') FROM sexed_tags),
  (SELECT COALESCE(array_agg(tag), '{}') FROM sexed_tags_ever),
  (SELECT COALESCE(array_agg(location_id::text ORDER BY location_id::text, partition_label), '{}') FROM sexed_buckets),
  (SELECT COALESCE(array_agg(partition_label ORDER BY location_id::text, partition_label), '{}') FROM sexed_buckets)`

	// The two bucket arrays are aggregated under the SAME ORDER BY over the same rows, so index
	// i names one bucket in both. Built any other way — one DISTINCT and its partner not, or two
	// differently ordered aggregates — every index would silently shift and pair a location with
	// another bucket's partition.
	if err := pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd, normalized, includeAllTime).Scan(
		&out.Tags, &out.AllTimeTags, &out.LocationIDs, &out.PartitionLabels,
	); err != nil {
		return SexScope{}, err
	}
	out.allTimeResolved = includeAllTime
	if len(out.LocationIDs) != len(out.PartitionLabels) {
		return SexScope{}, fmt.Errorf("weighing: sex scope bucket arrays disagree (%d locations, %d partitions)", len(out.LocationIDs), len(out.PartitionLabels))
	}
	return out, nil
}
