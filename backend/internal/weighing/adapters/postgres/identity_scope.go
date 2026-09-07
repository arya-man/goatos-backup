package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Same-animal keying for the Weights REPORTING screen.
//
// HERD JOIN BY RECORDED EXCEPTION (maintainer decision 2026-09-07). This is the FIFTH weighing
// file permitted to resolve a scanned tag to an animal, and the guard exempts it BY NAME. It is
// also the NARROWEST of the five: it reads `goat_identifiers` and NOTHING else — not `goats`, not
// `goat_shed_partitions`, not a procurement table. It never learns an animal's sex, breed, stage,
// pen or origin. It answers exactly one question and hands back exactly one answer:
//
//	"which of these scanned strings are the same animal?"
//
// WHY IT EXISTS. An animal on this farm can carry TWO RFIDs (`goat_identifiers.identifier_type`
// is `animal_identifier_1` or `animal_identifier_2`). Operators scan whichever tag they can read,
// and which one that is varies week to week. Every reporting read on the Weights and Growth
// surfaces keys an animal by the RAW SCANNED STRING, so an animal weighed on its primary tag in
// week 1 and its secondary in week 2 was TWO animals with ONE weigh each:
//
//   - it produced NO ADG at all — not a wrong one, a missing one — because every gain statistic
//     needs two weighs under one key. It vanished from the headline daily gain, the gain-by-breed
//     /sex/stage charts, the band board's moved-up/held/slipped-back and the losing-animals list.
//   - it counted TWICE in every `count(DISTINCT tag)` denominator, so the averages those counts
//     divide were wrong in the other direction at the same time.
//
// The farm cannot fix this by scanning more carefully: both tags are on the animal and both are
// legitimate. Only the herd register knows they are one animal.
//
// WHY ONE FILE AND NOT A JOIN IN EACH READ. Exactly the reasoning sex_scope.go records. The
// alternative was to let growth.go, shed_weights.go, load_weights.go, weight_demographics.go and
// the Growth Director reads each join `goat_identifiers`, which is the 2026-08-04 leak, five files
// over. Instead this file answers once and hands the others an OPAQUE PAIR OF PARALLEL ARRAYS of
// plain strings. Those files still name no herd table, still know nothing about animals, and would
// keep working unchanged if the herd register vanished — they would be handed an empty map.
//
// WHAT KEEPS IT SAFE, and what a future change must preserve:
//
//   - READ-ONLY and REPORTING-ONLY. No capture, submit, close or verdict path calls it. In
//     particular the one weighing business rule — an animal may not be scanned twice in the same
//     bucket before submit — still compares RAW STRINGS and is deliberately untouched: making it
//     identity-aware would gate a scan on the herd register, which is banned outright.
//   - NO scan is gated on identity. A tag that resolves to nothing is still recorded, still
//     counted, and simply keeps its own raw string as its key — exactly today's behaviour.
//   - AN ANIMAL WITH ONE PERMANENT RFID IS NEVER REMAPPED. The map carries rows ONLY for animals
//     holding two or more active permanent RFIDs, so a farm (or a window, or a test) with no
//     double-RFID animal gets an EMPTY map and every read runs the query it ran before this file
//     existed, key for key. That is what bounds the blast radius of the exception to the animals it
//     is about.
//   - THE CANONICAL KEY IS ONE OF THE ANIMAL'S OWN TAGS, never a goat_id. `animal_key` is rendered
//     to a reader verbatim as `ScannedIdentifier` in the losing-animals list, so a uuid there would
//     put a database key on a farm screen. It is the animal's `animal_identifier_1` where one
//     exists — the tag the farm calls primary — so the merged history reports under the tag a
//     reader expects, not under whichever tag happened to be scanned first.
//
// THE COST, STATED PLAINLY. Weighing now depends on herd identity data being right, which is the
// exact dependency growth.go's header comment refused in 2026-08-04. If the register wrongly
// attaches animal B's tag to animal A, their weights MERGE and the pair between them reports as
// growth that never happened. Two narrowings hold that down and both are load-bearing: only
// `status = 'active'` identifiers are read (`disputed`, `duplicate` and `invalid` are precisely
// the rows that would merge two animals, and they are the register's own way of saying "do not
// trust this"), and a tag is remapped only when the SAME goat carries another permanent RFID. A
// temporary_tag is a birth/provisional identity, not a second RFID slot, so a genuine re-tag still
// splits history. The exposure is therefore limited to double-tagged animals with an active,
// undisputed, wrong second RFID.
//
// Widening this exemption — another file, another table, or ANY write path — is a MAINTAINER
// decision, never a developer convenience.

// AnimalIdentityMap is the resolved answer to "which scanned strings are the same animal", in
// terms a weighing query can apply without knowing what an animal is.
//
// PARALLEL ARRAYS, zipped in SQL by `unnest($a, $b)`, for the same reason ReportScope's bucket
// arrays are parallel: they are two binds, and they are built in ONE pass from ONE ordered result
// set so an index can never pair a tag with another animal's canonical tag.
//
// It holds ONLY the tags of animals carrying two or more identifiers. A single-tag animal is
// absent, and absence means "key by the raw scanned string" — so an empty map is not "nothing
// matches", it is "nothing needs merging", and every read treats it as a no-op.
type AnimalIdentityMap struct {
	// Tags are normalized scanned identifiers (lower(btrim(...))) that belong to a multi-tag animal.
	Tags []string
	// CanonicalTags[i] is the ONE key every weigh of Tags[i]'s animal is grouped under. It is
	// itself a normalized tag of that same animal, so it is safe to show a reader.
	CanonicalTags []string
}

// EmptyAnimalIdentityMap is the no-op map: every read keys by the raw scanned string, which is
// what weighing did before this file existed. Callers that deliberately do not merge identities —
// and any caller in a test that is asserting raw-tag behaviour — pass this.
func EmptyAnimalIdentityMap() AnimalIdentityMap {
	return AnimalIdentityMap{Tags: []string{}, CanonicalTags: []string{}}
}

// Empty reports whether the map merges nothing. Unlike ReportScope.Empty this is unambiguous:
// there is no "filter applied but matched nothing" case here, because a map that matches nothing
// and a map that was never resolved mean the identical thing to every consumer — key by the raw
// tag. That is why this resolver has no `applied` flag and needs none.
func (m AnimalIdentityMap) Empty() bool { return len(m.Tags) == 0 }

// resolveAnimalIdentityMap resolves the map for one window and park scope.
//
// The window MUST be the read's full span INCLUDING its lookback: growth.go pairs a weigh inside
// the period against one up to `growthLookbackDays` before it, and a tag seen only in the lookback
// still has to merge or the pair this whole file exists to create is the one that goes missing.
// Callers pass lookbackStart, not periodStart.
//
// projection-review: membership=one row per active identifier of every animal that carries two or
// more active permanent RFID identifiers AND was weighed in scope through at least one of them;
// group_key=the normalized identifier value; join_cardinality=ident is 0..1 per tag because
// DISTINCT ON collapses re-issued rows for one tag string to the newest, weighed_goats is 1 row per
// goat_id (DISTINCT), and the final join is ident 1:1 back to its own goat; pagination=NONE,
// bounded by the tags actually weighed in the window and the two RFID slots each of their animals
// can carry; scope=tenant_id + park_id = ANY($2) + the window.
//
// Ratio key sets: none — this returns membership, not a ratio. `tag_count > 1` is a cap check over
// the SAME partition the canonical tag is chosen from, so a tag is remapped only when that very
// animal provably carries another permanent RFID.
func (r *Repository) resolveAnimalIdentityMap(ctx context.Context, tenantID string, parkIDs []string, from, to time.Time) (AnimalIdentityMap, error) {
	return ResolveAnimalIdentityMap(ctx, r.pool, tenantID, parkIDs, from, to)
}

// resolveAnimalIdentityMapAllTime resolves the map with NO time bound, for a page that carries a
// read spanning all history (today: the Growth screen, because sale readiness reports each
// animal's latest-EVER weight). A windowed map would leave such a read counting a double-tagged
// animal twice, with two "latest" weights, whenever both its tags were last scanned before the
// window -- the exact defect one grain over.
//
// This is the same trade sex_scope.go's AllTimeTags records: the unbounded arm is opt-in, so a
// windowed caller never pays for it, and the one page that asks already performs an unbounded scan
// of the same rows for sale readiness itself.
func (r *Repository) resolveAnimalIdentityMapAllTime(ctx context.Context, tenantID string, parkIDs []string) (AnimalIdentityMap, error) {
	return ResolveAnimalIdentityMap(ctx, r.pool, tenantID, parkIDs, time.Time{}, time.Time{})
}

// ResolveAnimalIdentityMap is the ONE implementation of the rule, callable with any pool so the
// Growth Director read in its own package merges the same animals the rest of the page does. Two
// surfaces reporting different herd growth is the cross-surface disagreement AGENTS.md bans, and
// a second copy of this rule is how that happens.
func ResolveAnimalIdentityMap(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkIDs []string, from, to time.Time) (AnimalIdentityMap, error) {
	out := EmptyAnimalIdentityMap()
	if len(parkIDs) == 0 {
		return out, nil
	}

	const q = ` -- scale-guard:ignore: bounded by the tags actually weighed in the selected window and by the at-most-two identifiers each of their animals carries; one reporting read per Weights page load, resolving no herd row for an animal nobody weighed
WITH scoped AS (
  SELECT cs.campaign_shed_id, cs.tenant_id
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c ON c.campaign_id = cs.campaign_id AND c.tenant_id = cs.tenant_id
  WHERE cs.tenant_id = $1::uuid AND c.park_id = ANY($2::uuid[]) AND cs.status <> 'canceled'
),
weighed AS (
  SELECT DISTINCT lower(btrim(o.scanned_identifier)) AS tag
  FROM weighing_observations o
  JOIN scoped s ON s.campaign_shed_id = o.campaign_shed_id AND s.tenant_id = o.tenant_id
  WHERE o.tenant_id = $1::uuid
    -- The window is OPT-IN: a zero time binds NULL and drops that side of the bound, which is how
    -- the all-time caller asks for every tag ever weighed in scope without a second query text.
    -- Written as an IS NULL disjunction rather than a sentinel date so "no bound" cannot be
    -- confused with a real date somebody typed.
    AND ($3::timestamptz IS NULL OR o.accepted_at >= $3::timestamptz)
    AND ($4::timestamptz IS NULL OR o.accepted_at < $4::timestamptz)
    AND o.verification_status <> 'rejected'
    AND btrim(o.scanned_identifier) <> ''
),
-- ONE goat per tag string. DISTINCT ON collapses a tag that was re-issued to the newest row, the
-- same convention weight_demographics.go and sex_scope.go already resolve identity with; keeping
-- three different answers to "whose tag is this" on one page is its own defect.
--
-- status = 'active' is the safety narrowing, not tidiness: 'disputed', 'duplicate' and 'invalid'
-- are the register's own record that an identifier is not to be trusted, and those are exactly the
-- rows that would merge two animals that are not one animal.
ident AS (
  SELECT DISTINCT ON (lower(btrim(gi.identifier_value)))
         lower(btrim(gi.identifier_value)) AS tag,
         gi.goat_id,
         gi.identifier_type
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
    AND gi.status = 'active'
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
    AND btrim(gi.identifier_value) <> ''
  ORDER BY lower(btrim(gi.identifier_value)), gi.created_at DESC
),
-- The animals actually weighed in scope, reached through WHICHEVER tag was scanned. Anchoring on
-- weighed tags rather than on the herd is what keeps this bounded: a park with 50,000 animals and
-- 300 weighs resolves at most 300 animals.
weighed_goats AS (
  SELECT DISTINCT i.goat_id
  FROM weighed w
  JOIN ident i ON i.tag = w.tag
),
-- EVERY active tag of those animals, including one that was never scanned in this window. That
-- tag costs nothing (no observation joins to it) and it keeps the canonical target STABLE: if the
-- primary tag is chosen only when it happens to have been scanned, the same animal's history
-- would report under a different key from one window to the next.
all_tags AS (
  SELECT i.tag,
         first_value(i.tag) OVER (
           PARTITION BY i.goat_id
           ORDER BY (i.identifier_type = 'animal_identifier_1') DESC, i.tag
         ) AS canonical_tag,
         count(*) OVER (PARTITION BY i.goat_id) AS tag_count
  FROM ident i
  JOIN weighed_goats wg ON wg.goat_id = i.goat_id
)
-- tag_count > 1 is what keeps a single-tag animal out of the map entirely, so a page with no
-- double-tagged animal is byte-for-byte the page that existed before this file. A tag whose
-- canonical tag is itself is kept when its animal has a sibling: dropping it would leave the
-- sibling merging onto a key the map never declares.
SELECT tag, canonical_tag
FROM all_tags
WHERE tag_count > 1
ORDER BY tag`

	// A zero time.Time binds as NULL, which the query reads as "no bound on that side". Passing the
	// zero value straight through would bind year 1 and quietly work today while being a lie.
	var fromArg, toArg any
	if !from.IsZero() {
		fromArg = from
	}
	if !to.IsZero() {
		toArg = to
	}
	rows, err := pool.Query(ctx, q, tenantID, parkIDs, fromArg, toArg)
	if err != nil {
		return AnimalIdentityMap{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag, canonical string
		if err := rows.Scan(&tag, &canonical); err != nil {
			return AnimalIdentityMap{}, err
		}
		out.Tags = append(out.Tags, tag)
		out.CanonicalTags = append(out.CanonicalTags, canonical)
	}
	if err := rows.Err(); err != nil {
		return AnimalIdentityMap{}, err
	}
	// The two arrays are appended in one pass over one ordered result set, so they can only
	// disagree if that loop is later restructured. Asserted rather than assumed, for the reason
	// assertBucketArraysAgree exists: a silent index shift pairs a tag with another animal's key
	// and merges the wrong two animals, which no downstream read can detect.
	if len(out.Tags) != len(out.CanonicalTags) {
		return AnimalIdentityMap{}, fmt.Errorf("weighing: animal identity map arrays disagree (%d tags, %d canonical)", len(out.Tags), len(out.CanonicalTags))
	}
	return out, nil
}

// animalKeyJoinSQL is the fragment every consuming read splices in beside its observation scan,
// with the two bind numbers that carry the map. It is a LEFT JOIN so an unmapped tag — a single-
// tag animal, or a scan the register does not know at all — keeps its own raw string, which is
// the free-flow guarantee this whole feature is built not to break.
//
// One helper rather than the fragment written out five times, because the shape MUST match the
// key expression below: a join that matched on the raw (untrimmed, uppercased) identifier while
// the key expression normalized it would silently merge nothing and look like it worked.
func animalKeyJoinSQL(alias, tagsParam, canonicalParam string) string {
	return fmt.Sprintf(
		"LEFT JOIN unnest(%s::text[], %s::text[]) AS akmap(tag, canonical_tag) ON akmap.tag = lower(btrim(%s.scanned_identifier))",
		tagsParam, canonicalParam, alias,
	)
}

// animalKeySQL is the identity expression itself: the animal's canonical tag when the map names
// one, and otherwise the raw scanned string exactly as before.
func animalKeySQL(alias string) string {
	return fmt.Sprintf("COALESCE(akmap.canonical_tag, lower(btrim(%s.scanned_identifier)))", alias)
}
