package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Origin scoping for the Weights REPORTING screen: FARM BORN vs PURCHASED.
//
// The maintainer asked for a second cohort filter beside Sex (2026-09-01): the farm buys kids in
// loads and also breeds its own, and the two grow differently enough that reading them together
// answers nothing. "In loads we have data ... Castro 1, 2, 3 in CBE and Castro 1 and Castro 2 and
// Godel 2 - Part 1 and 2 are purchased only."
//
// HERD JOIN BY RECORDED EXCEPTION (maintainer decision 2026-09-01). This is the FOURTH weighing
// file permitted to resolve a scanned tag to an animal, and the guard exempts it BY NAME.
//
// IT DID NOT START THAT WAY, and the reason it changed is the whole rule. The first version
// answered "was this PEN bought" from `weighing_shed_load_tags` (000131) -- a weighing-owned,
// location-grain mapping -- and so needed no exception at all. That is correct for the seven pens
// the maintainer named, whose every resident came off a load. It is wrong for Channapatna's
// Mandela 1 - Part 1, which holds 13 kids of which only 4 were bought: judging a scanned weigh by
// its pen filed all 12 of that pen's scanned kids as purchased. The maintainer caught it on the
// first run -- "whole mandela 1 - part 1 are not purchased only few are purchased in that right??"
//
// A pen is the only thing a whole-shed weigh CAN be judged by; it carries no tags. But a scanned
// weigh carries one, so it can be answered per ANIMAL, and `procurement_load_goats` is the only
// table that says which animal came off which load. Hence the exception. It buys real accuracy:
// the pen-level rule cannot be made correct for a mixed pen at any price.
//
// This file therefore reads:
//
//	weighing_campaign_sheds / weighing_campaigns / weighing_observations   weighing-owned
//	goat_identifiers, goats, goat_shed_partitions                          herd, BY EXCEPTION
//	procurement_load_goats                                                 procurement, BY EXCEPTION
//	locations                                                              ORG, already allowed
//
// `procurement_load_goats` is allowlisted for THIS FILE ONLY -- the guard's exemption is keyed per
// file precisely so a procurement table cannot leak into the other three exempt files, none of
// which has any business asking where an animal was bought.
//
// WHAT KEEPS IT SAFE, same three properties as the Sex filter:
//   - READ-ONLY and REPORTING-ONLY. No capture, submit, close or verdict path calls it.
//   - NO scan is gated on origin. A weigh in an untagged pen is still recorded and still counted;
//     this narrows what a REPORT counts, never what the field may capture.
//   - An empty origin returns an EMPTY scope and every caller reads that as "no filter", so the
//     unfiltered page runs the query it ran before this file existed.
const (
	// OriginFarmBorn is an animal with no procurement load row. It is the COMPLEMENT of purchased
	// rather than a positively recorded fact: the farm records what it buys, not what it breeds, so
	// "no load row" is the only evidence of farm birth there is. An animal whose tag resolves to
	// nothing has no load row EITHER, which is why an unresolved tag is claimed by neither side
	// instead of falling into this one by default.
	OriginFarmBorn = "farm_born"
	// OriginPurchased is an animal carrying a procurement load row, or a whole-shed pen every one
	// of whose live residents carries one.
	OriginPurchased = "purchased"
)

// normalizeOriginFilter accepts the two cohorts and rejects everything else, rather than passing
// an arbitrary string into a predicate. An unknown value is an error, never a silent "no filter":
// silently widening a filter shows a reader more kids than they asked for under a heading that
// says otherwise. Wrapped in ErrInvalidArgument so the HTTP layer answers 400 rather than 500 —
// a caller who mistypes a filter has made a bad REQUEST, and telling them the server broke sends
// them looking in the wrong place.
func normalizeOriginFilter(origin string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(origin)) {
	case "":
		return "", nil
	case OriginFarmBorn:
		return OriginFarmBorn, nil
	case OriginPurchased:
		return OriginPurchased, nil
	default:
		return "", fmt.Errorf("%w: unsupported origin filter %q", ports.ErrInvalidArgument, origin)
	}
}

func (r *Repository) resolveOriginScope(ctx context.Context, tenantID string, parkIDs []string, origin string, periodStart, periodEnd time.Time) (ReportScope, error) {
	return resolveOriginScope(ctx, r.pool, tenantID, parkIDs, origin, periodStart, periodEnd, false)
}

func (r *Repository) resolveOriginScopeWithAllTime(ctx context.Context, tenantID string, parkIDs []string, origin string, periodStart, periodEnd time.Time) (ReportScope, error) {
	return resolveOriginScope(ctx, r.pool, tenantID, parkIDs, origin, periodStart, periodEnd, true)
}

// ResolveOriginScope is the ONE implementation of the rule, callable with any pool so the Growth
// Director read in its own package classifies the same pens the rest of the page does. Two copies
// of "which pens were bought" would drift, and one of the two would be the one a reader is
// looking at.
func ResolveOriginScope(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, origin string, periodStart, periodEnd time.Time) (ReportScope, error) {
	return resolveOriginScope(ctx, pool, tenantID, parkIDs, origin, periodStart, periodEnd, false)
}

// ResolveOriginScopeWithAllTime resolves the same scope PLUS AllTimeTags, for a caller whose read
// is deliberately not windowed (today: sale readiness, which reports latest-EVER weights).
func ResolveOriginScopeWithAllTime(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, origin string, periodStart, periodEnd time.Time) (ReportScope, error) {
	return resolveOriginScope(ctx, pool, tenantID, parkIDs, origin, periodStart, periodEnd, true)
}

// resolveOriginScope resolves the filter for one window and park scope.
//
// An empty origin short-circuits WITHOUT running the query at all, so the unfiltered page — which
// is every page load until a reader picks a side — pays nothing for this file existing.
//
// projection-review: membership=one row per DISTINCT normalized tag weighed in the window (plus the 90-day gain lookback) whose animal answers this origin, and one row per scoped lump-sum bucket whose live residents ALL answer it; group_key=the normalized tag, and (location_id, partition_label) for buckets; join_cardinality=ident 0..1 per tag because DISTINCT ON collapses re-issued identifier rows to the newest, bought is 0..1 per goat because it is SELECT DISTINCT goat_id, goats 1 per goat_id (PK), residents 0..N COLLAPSED by the HAVING aggregate; pagination=NONE, bounded by the tags weighed in the window and by the scoped buckets; scope=tenant_id + park_id = ANY($2) + the window.
//
//	PRODUCER UNIQUENESS vs CONSUMER MATCH KEYS, side by side:
//	  weighed        unique on tag                          [its own SELECT DISTINCT]
//	  ident          unique on tag                          [DISTINCT ON (tag)]
//	  bought         unique on goat_id                      [SELECT DISTINCT goat_id]
//	  origin_tags    unique on tag                          [1:1 with weighed, EXISTS adds no rows]
//	  origin_buckets unique on (location_id, partition_label) [its own GROUP BY]
//
//	ROW MULTIPLICITY OF EVERY JOINED SIDE:
//	  bought                  0..1 per animal, DISTINCT so a two-load animal cannot double it
//	  goats / goat_shed_partitions  0..N per bucket, COLLAPSED by the GROUP BY
//
//	RATIO KEY SETS, shown identical: the HAVING compares
//	  count(*) FILTER (WHERE bought)  against  count(*)
//	over the SAME grouped row set -- same FROM, same GROUP BY, no branch adds a join -- so
//	"all bought" provably means every row counted, and "none bought" provably means no row did.
func resolveOriginScope(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, origin string, periodStart, periodEnd time.Time, includeAllTime bool) (ReportScope, error) {
	out := ReportScope{Tags: []string{}, AllTimeTags: []string{}, LocationIDs: []string{}, PartitionLabels: []string{}}
	normalized, err := normalizeOriginFilter(origin)
	if err != nil {
		return ReportScope{}, err
	}
	if normalized == "" || len(parkIDs) == 0 {
		return out, nil
	}

	const q = ` -- scale-guard:ignore: bounded by the selected window and park list; one reporting read per Weights page load, resolving only the tags actually weighed and the lump-sum buckets actually scoped. The one unwindowed arm is OPT-IN behind $6 and executes as a One-Time Filter for every caller that does not ask, exactly as the sibling sex scope does
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.location_id, COALESCE(cs.partition_label, '') AS partition_label, cs.weighing_category
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
),
-- The 90-day lookback matches the gain reads: a kid's PREVIOUS weigh can sit before the selected
-- window, and dropping its tag here would silently remove that kid's daily gain from a filtered
-- page while leaving it on the unfiltered one.
weighed AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  WHERE o.tenant_id = $1::uuid
    AND o.accepted_at >= ($3::timestamptz - interval '90 days') AND o.accepted_at < $4::timestamptz
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
),
-- THE SAME TAGS, WITH NO TIME BOUND, for the one read that is deliberately not windowed: sale
-- readiness reports each animal's LATEST-EVER weight, so bounding it to the window would hide
-- exactly the animal the question is about. OPT-IN behind $6, which is what keeps the all-history
-- scan free for the windowed reads that share this resolver and never read the result.
weighed_ever AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id
  WHERE $6::bool
    AND o.tenant_id = $1::uuid
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
-- WAS THIS ANIMAL BOUGHT? The set of animals appearing on any procurement load.
--
-- A load row is per (load, animal) and an animal can legitimately sit on more than one load line --
-- Channapatna's Mandela 1 - Part 1 is under loads 100 and 101. Both readers below consult this
-- through EXISTS, which is a SEMI-join and so already collapses those duplicates; the DISTINCT is
-- here to make the CTE a genuine set rather than because it is load-bearing today. Say so plainly:
-- a future change that turns either EXISTS into a JOIN would fan a two-load animal out and quietly
-- break the all-bought test below, and the DISTINCT is what would keep that honest.
bought AS (
  SELECT DISTINCT goat_id FROM procurement_load_goats WHERE tenant_id = $1::uuid
),
-- AN INDIVIDUAL WEIGH IS CLAIMED THROUGH ITS OWN ANIMAL, not through the pen it happened in.
--
-- This is the correction the maintainer made on 2026-09-01 after the pen-level rule shipped:
-- Channapatna's Mandela 1 - Part 1 holds 13 kids of which only 4 came from a load, and judging a
-- scanned weigh by its pen filed all 12 of that pen's scanned kids as purchased. A scanned weigh
-- CARRIES a tag, so it can be answered per animal, and a mixed pen is only a problem for the
-- whole-shed arm below, which has no tags to answer with.
--
-- INNER JOIN on ident, so a tag that resolves to NO animal is claimed by NEITHER side. It is still
-- recorded and still counted in the unfiltered view -- it simply cannot answer a question about
-- where the animal came from. The filtered halves therefore do not add up to the unfiltered total,
-- and that gap is honest rather than missing data; it is the same rule the Sex filter follows.
origin_tags AS (
  SELECT w.tag
  FROM weighed w
  JOIN ident i ON i.tag = w.tag
  WHERE (EXISTS (SELECT 1 FROM bought b WHERE b.goat_id = i.goat_id)) = ($5::text = ` + "'" + OriginPurchased + "'" + `)
),
origin_tags_ever AS (
  SELECT w.tag
  FROM weighed_ever w
  JOIN ident i ON i.tag = w.tag
  WHERE (EXISTS (SELECT 1 FROM bought b WHERE b.goat_id = i.goat_id)) = ($5::text = ` + "'" + OriginPurchased + "'" + `)
),
-- A whole-shed weigh has no tags, so it is attributed by the cohort its pen holds. The bucket
-- points at a PARTITION (Castro 1) but the herd register puts the animals on the physical shed
-- (Castro), so each bucket resolves to its partition's own residents when it has any and otherwise
-- to its parent shed's -- the same resolution the sex scope and the demographics read use, for the
-- same reason: looking only at the partition finds nothing and silently drops every whole-shed
-- weigh.
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
-- AGREE OR GO TO NEITHER SIDE. A pen is claimed only when EVERY live resident answers the same
-- way: all bought, or none bought. A pen holding both -- Mandela 1 - Part 1 is 4 of 13 -- has one
-- average weight that cannot be divided between the two cohorts, so splitting it would invent a
-- distribution nobody measured and claiming it whole would put nine farm-born kids in the bought
-- column. It is claimed by neither, which is the identical rule the Sex filter applies to a shed
-- holding both sexes.
origin_buckets AS (
  SELECT src.location_id, src.partition_label
  FROM shed_targets src
  JOIN goats g ON g.shed_id = src.resolved_id AND g.tenant_id = $1::uuid
   AND g.lifecycle_status = 'alive'
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  -- MATCH ON THE SCRUBBED KEY, NOT THE LABEL. The bucket's location is named "Godel 2 - Part 1",
  -- from which the partition extracts as "1", while the register writes the human label "Part 1"
  -- on the goat. Comparing those two strings raw matches NOTHING, so a real pen would be claimed
  -- by neither side and its kids would fall out of both halves of the page with nothing on screen
  -- to say why. Both sides are reduced to the same key here.
  WHERE src.resolved_partition_label = ''
     OR regexp_replace(lower(btrim(gsp.partition_label)), '^(part|pt)[\s.-]*', '')
        = regexp_replace(lower(btrim(src.resolved_partition_label)), '^(part|pt)[\s.-]*', '')
  GROUP BY src.location_id, src.partition_label
  HAVING count(*) > 0
     AND CASE WHEN $5::text = ` + "'" + OriginPurchased + "'" + `
              -- ALL bought: every resident carries a load row.
              THEN count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bought b WHERE b.goat_id = g.goat_id)) = count(*)
              -- NONE bought.
              ELSE count(*) FILTER (WHERE EXISTS (SELECT 1 FROM bought b WHERE b.goat_id = g.goat_id)) = 0
         END
)
SELECT
  (SELECT COALESCE(array_agg(tag), '{}') FROM origin_tags),
  (SELECT COALESCE(array_agg(tag), '{}') FROM origin_tags_ever),
  (SELECT COALESCE(array_agg(location_id::text ORDER BY location_id::text, partition_label), '{}') FROM origin_buckets),
  (SELECT COALESCE(array_agg(partition_label ORDER BY location_id::text, partition_label), '{}') FROM origin_buckets)`

	// The two bucket arrays are aggregated under the SAME ORDER BY over the same rows, so index i
	// names one bucket in both.
	if err := pool.QueryRow(ctx, q, tenantID, parkIDs, periodStart, periodEnd, normalized, includeAllTime).Scan(
		&out.Tags, &out.AllTimeTags, &out.LocationIDs, &out.PartitionLabels,
	); err != nil {
		return ReportScope{}, err
	}
	out.allTimeResolved = includeAllTime
	if err := assertBucketArraysAgree("origin scope", out); err != nil {
		return ReportScope{}, err
	}
	return out, nil
}
