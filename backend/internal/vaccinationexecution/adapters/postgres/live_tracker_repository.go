package postgres

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	vaccinatdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// istZone is the business timezone every "drive day" boundary in this file is cut on. It is spelled
// once here rather than inline in six SQL strings, because a single divergent copy would silently
// move one section of the page onto a different day than the rest.
const istZone = "Asia/Kolkata"

// liveTrackerVaccineFamilyExpr reduces a protocol dose code to its antigen family, which is the
// grain the tracker's vaccine axis works at ("goat_pox_adult_w1" -> "goat_pox"). It mirrors
// domain.VaccineFamilyCode exactly; the Go copy labels, the SQL copy filters and groups.
//
// public.vaccines is empty in every environment, so there is no catalog table to join and the family
// prefix is the only vaccine identity available.
const liveTrackerVaccineFamilyExpr = `
  CASE
    WHEN lower(btrim(pr.dose_code)) ~ '^blue_tongue(_|$)' THEN 'blue_tongue'
    WHEN lower(btrim(pr.dose_code)) ~ '^sheep_pox(_|$)'   THEN 'sheep_pox'
    WHEN lower(btrim(pr.dose_code)) ~ '^goat_pox(_|$)'    THEN 'goat_pox'
    WHEN lower(btrim(pr.dose_code)) ~ '^et_tt(_|$)'       THEN 'et_tt'
    WHEN lower(btrim(pr.dose_code)) ~ '^ppr(_|$)'         THEN 'ppr'
    WHEN lower(btrim(pr.dose_code)) ~ '^fmd(_|$)'         THEN 'fmd'
    WHEN lower(btrim(pr.dose_code)) ~ '^hs(_|$)'          THEN 'hs'
    ELSE split_part(lower(btrim(pr.dose_code)), '_', 1)
  END`

// liveTrackerPartitionNormExpr is the ONE partition-identity normalizer used on BOTH sides of every
// partition join in this file.
//
// goat_shed_partitions stores Old Yashoda as "1".."4" while vaccination_drive_assignments stores the
// same partitions as "Part 1".."Part 4"; Gandhi uses bare "2"/"3" on both sides and Godel 1 uses
// "Part N" on both. Joining raw labels does not error — it quietly drops an entire park's partitions
// into "unassigned", which reads on screen as an operator who was never given any work.
func liveTrackerPartitionNormExpr(column string) string {
	return fmt.Sprintf(`regexp_replace(lower(btrim(COALESCE(%s, 'whole'))), '^part[[:space:]]*', '')`, column)
}

// liveTrackerScopedCTE is the SINGLE membership definition every section of the live drive tracker
// derives from. The mock promises "Filters apply to tiles, tables and the live feed together"; that
// promise is only keepable if the tiles, both tables, the combo card, the feed, the attention list
// and the verification block all count rows out of the same filtered set. Assembling them from
// separate queries with separate WHERE clauses is exactly how the mock ended up with tiles that do
// not add up to its own tables.
//
// Grain: ONE obligation_instance = ONE administration. Animals are counted only where the contract
// says "animals" (combo.animal_count); the two are never mixed inside one number.
//
// Effective drive date: an obligation's drive day is COALESCE(active drive-date override, the IST
// date of due_at) — the same override chain driveAssignmentsSQL applies to assignments, applied here
// at obligation grain instead of being re-invented. The override is keyed by (tenant, park, vaccine
// code, original date) and only fires when it is not canceled.
//
// projection-review: membership=obligation_instances for one tenant whose protocol_definitions.category is 'vaccination' (the obligation engine is shared with deworming, biosecurity and the rest) and whose EFFECTIVE drive date (active vaccination_drive_date_overrides else the IST date of due_at) equals the requested business date, excluding the dead statuses canceled/superseded/waived; group_key=(park_id, shed_id, normalized partition_label, vaccine family, assigned operator_id) for the board sections and (goat_id) for the combo/proof sections; join_cardinality=goat/partition/protocol joins are keyed 1:1 by tenant plus stable id, the drive assignment is resolved through a LIMIT 1 LATERAL so a duplicate assignment row cannot fan an obligation out, the proof/scan/attempt day tables are pre-aggregated to one row per goat BEFORE being joined, the per-actor evidence CTEs join a DISTINCT goat set (scoped_goats) rather than the per-obligation set, the goat-keyed feed arms join a one-row-per-goat projection (goat_places) rather than the dose-grain shed_names, and workforce_members is resolved through a status='active' LIMIT 1 LATERAL because only the active row is unique per user_id — so no evidence count and no feed row can multiply on a combo animal or a re-hired person; pagination=every returned array is bounded server-side (cells<=2000 with cells_truncated reported, operators<=100 and sheds<=200 with totals and truncation flags reported, combo rows<=200 with an exact total, filter options<=1000, activity keyset-paginated by occurred_at) and no COUNT(*) runs over per-goat rows outside this date-scoped membership; scope=tenant plus the backend-clamped park filter, then optional shed / normalized partition / operator / vaccine-family narrowing applied inside the CTE so every downstream section inherits it.
var liveTrackerScopedCTE = `
WITH day_proofs AS (
  SELECT
    pa.subject_id AS goat_id,
    count(*) FILTER (WHERE pa.upload_state = 'completed')::int AS completed_count,
    count(*) FILTER (WHERE pa.upload_state IN ('pending', 'uploading'))::int AS pending_count,
    max(COALESCE(pa.uploaded_at, pa.created_at)) FILTER (WHERE pa.upload_state = 'completed') AS last_proof_at
  FROM proof_artifacts pa
  JOIN sop_tasks st
    ON st.tenant_id = pa.tenant_id
   AND st.task_id = pa.scope_id
  WHERE pa.tenant_id = $1::uuid
    AND pa.scope_type = 'task'
    AND st.task_type = 'vaccination'
    AND pa.subject_type = 'goat'
    AND pa.subject_id IS NOT NULL
    AND pa.proof_type = 'video'
    AND (COALESCE(pa.uploaded_at, pa.created_at) AT TIME ZONE '` + istZone + `')::date = $2::date
  GROUP BY pa.subject_id
),
day_scans AS (
  SELECT
    c.goat_id,
    count(*)::int AS scan_count,
    max(c.captured_at) AS last_scan_at
  FROM sop_task_scan_captures c
  JOIN sop_tasks st
    ON st.tenant_id = c.tenant_id
   AND st.task_id = c.task_id
  WHERE c.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND c.goat_id IS NOT NULL
    AND (c.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date
  GROUP BY c.goat_id
),
day_attempts AS (
  SELECT
    a.goat_id,
    count(*)::int AS extra_count,
    max(a.captured_at) AS last_attempt_at
  FROM sop_task_scan_attempts a
  JOIN sop_tasks st
    ON st.tenant_id = a.tenant_id
   AND st.task_id = a.task_id
  WHERE a.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND a.goat_id IS NOT NULL
    AND a.outcome IN ('duplicate', 'not_due', 'unknown')
    AND (a.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date
  GROUP BY a.goat_id
),
scoped AS (
  SELECT
    oi.obligation_id,
    oi.target_id AS goat_id,
    oi.rule_id,
    oi.status,
    oi.completed_at,
    g.shed_id,
    g.park_id,
    COALESCE(gsp.partition_label, 'whole') AS partition_label,
    ` + liveTrackerPartitionNormExpr("gsp.partition_label") + ` AS part_norm,
    pr.dose_code,
    pd.name AS protocol_name,` + liveTrackerVaccineFamilyExpr + ` AS vaccine_family
  FROM obligation_instances oi
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = oi.tenant_id
   AND gsp.goat_id = g.goat_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  JOIN protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   -- The obligation engine is SHARED across every protocol category (deworming, biosecurity, feed
   -- water testing, panel cleaning, ...). Without this predicate a goat-targeted deworming
   -- obligation lands in the scoped membership, is counted into "Scheduled today", and can
   -- NEVER be proofed off — every evidence CTE below filters st.task_type = 'vaccination'. It would
   -- inflate Remaining permanently and pin its shed row at not_started forever.
   AND pd.category = 'vaccination'
  LEFT JOIN LATERAL (
    SELECT MIN(NULLIF(dim.vaccine_code, '')) AS vaccine_code
    FROM protocol_rule_dimensions dim
    WHERE dim.tenant_id = pr.tenant_id
      AND dim.rule_id = pr.rule_id
  ) prd ON true
  LEFT JOIN vaccination_drive_date_overrides ovr
    ON ovr.tenant_id = oi.tenant_id
   AND ovr.park_id = g.park_id
   AND ovr.original_drive_date = (oi.due_at AT TIME ZONE '` + istZone + `')::date
   AND lower(btrim(ovr.vaccine_code)) = lower(btrim(NULLIF(prd.vaccine_code, '')))
   AND ovr.canceled_at IS NULL
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    -- 'superseded' and 'waived' are DEAD obligations: they will never receive a proof. Counting them
    -- into the scheduled count inflates the Scheduled tile and Remaining, and holds the shed row at
    -- not_started for the rest of the day. Same exclusion set as repository.go's execution reads.
    AND oi.status NOT IN ('canceled', 'superseded', 'waived')
    AND COALESCE(ovr.override_date, (oi.due_at AT TIME ZONE '` + istZone + `')::date) = $2::date
    AND ($3::text = '' OR g.park_id::text = $3::text)
),
scoped_enriched AS (
  SELECT
    s.*,
    asg.operator_id,
    COALESCE(dp.completed_count, 0) AS proof_completed_count,
    COALESCE(dp.pending_count, 0) AS proof_pending_count,
    dp.last_proof_at,
    COALESCE(ds.scan_count, 0) AS scan_count,
    ds.last_scan_at,
    COALESCE(da.extra_count, 0) AS extra_count,
    da.last_attempt_at
  FROM scoped s
  LEFT JOIN LATERAL (
    SELECT a.operator_id
    FROM vaccination_drive_assignments a
    WHERE a.tenant_id = $1::uuid
      AND a.planned_date = $2::date
      AND a.shed_id = s.shed_id
      AND ` + liveTrackerPartitionNormExpr("a.partition_label") + ` = s.part_norm
    ORDER BY a.assignment_id
    LIMIT 1
  ) asg ON true
  LEFT JOIN day_proofs dp ON dp.goat_id = s.goat_id
  LEFT JOIN day_scans ds ON ds.goat_id = s.goat_id
  LEFT JOIN day_attempts da ON da.goat_id = s.goat_id
  WHERE ($4::text = '' OR s.shed_id::text = $4::text)
    AND ($5::text = '' OR s.part_norm = $5::text)
    AND ($6::text = '' OR asg.operator_id::text = $6::text)
    AND ($7::text = '' OR s.vaccine_family = $7::text)
)`

// liveTrackerCellsSQL is the board's one aggregate: the day's administrations rolled up to
// park × shed × partition × vaccine family × assigned operator. Every KPI tile, both tables, the
// attention list and the park split are folded out of THIS result set in Go, so a tile can never
// disagree with the table under it.
var liveTrackerCellsSQL = liveTrackerScopedCTE + `
SELECT
  se.park_id::text,
  COALESCE(pk.name, ''),
  COALESCE(pk.location_code, ''),
  se.shed_id::text,
  COALESCE(sh.name, ''),
  se.partition_label,
  se.part_norm,
  se.vaccine_family,
  min(se.dose_code) AS dose_code,
  min(se.protocol_name) AS protocol_name,
  COALESCE(se.operator_id::text, ''),
  COALESCE(wm.display_name, ''),
  COALESCE(wm.display_code, ''),
  count(*)::int AS scheduled,
  count(*) FILTER (WHERE se.proof_completed_count > 0)::int AS proofed,
  count(*) FILTER (WHERE se.proof_completed_count = 0 AND se.proof_pending_count > 0)::int AS uploading,
  count(*) FILTER (WHERE se.scan_count > 0)::int AS scanned,
  COALESCE(sum(se.extra_count), 0)::int AS extra_attempts,
  max(se.last_proof_at) AS last_proof_at,
  GREATEST(max(se.last_proof_at), max(se.last_scan_at), max(se.last_attempt_at)) AS last_activity_at
FROM scoped_enriched se
LEFT JOIN locations pk
  ON pk.tenant_id = $1::uuid
 AND pk.location_id = se.park_id
LEFT JOIN locations sh
  ON sh.tenant_id = $1::uuid
 AND sh.location_id = se.shed_id
LEFT JOIN workforce_members wm
  ON wm.tenant_id = $1::uuid
 AND wm.workforce_member_id = se.operator_id
GROUP BY se.park_id, pk.name, pk.location_code, se.shed_id, sh.name, se.partition_label, se.part_norm,
         se.vaccine_family, se.operator_id, wm.display_name, wm.display_code
ORDER BY pk.name, sh.name, se.part_norm, se.vaccine_family
LIMIT ` + fmt.Sprint(domain.LiveTrackerMaxCells)

// liveTrackerActorSQL attributes evidence to the person who actually produced it. Videos and scans
// are credited through proof_artifacts.uploaded_by / sop_task_scan_captures.captured_by, both of
// which join workforce_members on user_id — NEVER workforce_member_id. Joining the wrong column does
// not error; it returns zero rows and silently blanks every operator name on the board.
//
// Both evidence CTEs join scoped_GOATS, never scoped_enriched. scoped_enriched is ONE ROW PER
// OBLIGATION, so a combo animal carrying two same-day obligations would join each of its proof and
// scan rows twice and DOUBLE the Videos and Scans columns — which then feeds Remaining, the
// operator's live state, the idle attention rows and the Attention tile.
var liveTrackerActorSQL = liveTrackerScopedCTE + `,
scoped_goats AS (
  SELECT DISTINCT goat_id FROM scoped_enriched
),
actor_proofs AS (
  SELECT pa.uploaded_by AS actor_id, count(*)::int AS videos, max(COALESCE(pa.uploaded_at, pa.created_at)) AS last_at
  FROM proof_artifacts pa
  JOIN sop_tasks st ON st.tenant_id = pa.tenant_id AND st.task_id = pa.scope_id
  JOIN scoped_goats sg ON sg.goat_id = pa.subject_id
  WHERE pa.tenant_id = $1::uuid
    AND pa.scope_type = 'task'
    AND st.task_type = 'vaccination'
    AND pa.subject_type = 'goat'
    AND pa.proof_type = 'video'
    AND pa.upload_state = 'completed'
    AND pa.uploaded_by IS NOT NULL
    AND (COALESCE(pa.uploaded_at, pa.created_at) AT TIME ZONE '` + istZone + `')::date = $2::date
  GROUP BY pa.uploaded_by
),
actor_scans AS (
  SELECT c.captured_by AS actor_id, count(*)::int AS scans, max(c.captured_at) AS last_at
  FROM sop_task_scan_captures c
  JOIN sop_tasks st ON st.tenant_id = c.tenant_id AND st.task_id = c.task_id
  JOIN scoped_goats sg ON sg.goat_id = c.goat_id
  WHERE c.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND c.captured_by IS NOT NULL
    AND (c.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date
  GROUP BY c.captured_by
),
actors AS (
  SELECT actor_id FROM actor_proofs
  UNION
  SELECT actor_id FROM actor_scans
)
SELECT
  COALESCE(wm.workforce_member_id::text, ''),
  a.actor_id::text,
  COALESCE(wm.display_name, ''),
  COALESCE(wm.display_code, ''),
  COALESCE(ap.videos, 0),
  COALESCE(asn.scans, 0),
  GREATEST(ap.last_at, asn.last_at) AS last_activity_at
FROM actors a
LEFT JOIN actor_proofs ap ON ap.actor_id = a.actor_id
LEFT JOIN actor_scans asn ON asn.actor_id = a.actor_id
-- workforce_members is only UNIQUE on (tenant_id, user_id) WHERE status = 'active' (partial index
-- workforce_members_active_user_unique_idx). A re-hired person legitimately holds one active row
-- plus one or more left/inactive rows on the same user_id, so a bare join fans this SELECT out and
-- the operator board grows a second row for the same human with the same counts.
LEFT JOIN LATERAL (
  SELECT m.workforce_member_id, m.display_name, m.display_code
  FROM workforce_members m
  WHERE m.tenant_id = $1::uuid
    AND m.user_id = a.actor_id
    AND m.status = 'active'
  ORDER BY m.workforce_member_id
  LIMIT 1
) wm ON true
LIMIT 500`

// liveTrackerComboSQL returns the animals carrying two or more DISTINCT same-day vaccination rules —
// the "one proof, two obligations" case. animal_count is the exact total; rows are capped so a combo
// day across the whole herd cannot stream every animal into the page.
var liveTrackerComboSQL = liveTrackerScopedCTE + `,
combo_goats AS (
  SELECT goat_id
  FROM scoped_enriched
  GROUP BY goat_id
  HAVING count(DISTINCT rule_id) > 1
),
combo_total AS (SELECT count(*)::int AS animal_count FROM combo_goats),
combo_page AS (SELECT goat_id FROM combo_goats ORDER BY goat_id LIMIT $8::int)
SELECT
  (SELECT animal_count FROM combo_total),
  se.goat_id::text,
  COALESCE(gt.display_id, ''),
  COALESCE(ident.primary_tag, ''),
  COALESCE(ident.secondary_tag, ''),
  COALESCE(sh.name, ''),
  se.partition_label,
  se.obligation_id::text,
  se.vaccine_family,
  se.dose_code,
  se.protocol_name,
  se.status,
  se.proof_completed_count,
  se.proof_pending_count
FROM scoped_enriched se
JOIN combo_page cp ON cp.goat_id = se.goat_id
LEFT JOIN goats gt ON gt.tenant_id = $1::uuid AND gt.goat_id = se.goat_id
LEFT JOIN locations sh ON sh.tenant_id = $1::uuid AND sh.location_id = se.shed_id
LEFT JOIN LATERAL (
  SELECT
    max(gi.identifier_value) FILTER (WHERE gi.identifier_type = 'animal_identifier_1') AS primary_tag,
    max(gi.identifier_value) FILTER (WHERE gi.identifier_type = 'animal_identifier_2') AS secondary_tag
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
    AND gi.goat_id = se.goat_id
    AND gi.status = 'active'
) ident ON true
ORDER BY se.goat_id, se.vaccine_family, se.dose_code`

// liveTrackerActivitySQL is the live feed. It is a UNION of the five canonical event tables, NOT
// audit_log: today's audit rows carry obligation lifecycle transitions only, no scan and no proof
// event, so /operations/audit cannot back this feed at all.
//
// Identifiers rendered here are the REAL scanned tag or the animal's active primary identifier. No
// display id is ever synthesised.
var liveTrackerActivitySQL = liveTrackerScopedCTE + `,
-- shed_names is DOSE grain (one row per goat × dose). It is only safe to join from an arm that is
-- itself dose-keyed, which is why the administration and shed_submitted arms below join on
-- goat_id AND dose_code.
shed_names AS (
  SELECT DISTINCT se.goat_id, se.shed_id, se.partition_label, se.vaccine_family, se.dose_code, se.protocol_name, se.park_id
  FROM scoped_enriched se
),
-- goat_places is GOAT grain — exactly one row per animal. The proof, scan-capture and scan-attempt
-- arms are keyed on goat alone, so joining them to the dose-grain CTE emitted TWO feed rows per
-- physical event for a combo animal, both carrying the SAME event_id: duplicate React keys, a
-- LIMIT consumed by duplicates, and an observed_per_min inflated by the multiplicity.
goat_places AS (
  SELECT DISTINCT ON (se.goat_id)
    se.goat_id, se.shed_id, se.partition_label, se.park_id, se.dose_code, se.protocol_name
  FROM scoped_enriched se
  ORDER BY se.goat_id, se.dose_code
),
events AS (
  SELECT
    'proof_video:' || pa.proof_id::text AS event_id,
    COALESCE(pa.uploaded_at, pa.created_at) AS occurred_at,
    'proof_video' AS kind,
    pa.uploaded_by AS actor_id,
    se.goat_id,
    se.shed_id,
    se.partition_label,
    se.park_id,
    se.dose_code,
    se.protocol_name,
    ''::text AS scanned_identifier,
    ''::text AS detail_code
  FROM proof_artifacts pa
  JOIN sop_tasks st ON st.tenant_id = pa.tenant_id AND st.task_id = pa.scope_id
  JOIN goat_places se ON se.goat_id = pa.subject_id
  WHERE pa.tenant_id = $1::uuid
    AND pa.scope_type = 'task'
    AND st.task_type = 'vaccination'
    AND pa.subject_type = 'goat'
    AND pa.proof_type = 'video'
    AND pa.upload_state = 'completed'
    AND (COALESCE(pa.uploaded_at, pa.created_at) AT TIME ZONE '` + istZone + `')::date = $2::date

  UNION ALL

  SELECT
    'scan_capture:' || c.capture_id::text,
    c.captured_at,
    'scan_capture',
    c.captured_by,
    se.goat_id,
    se.shed_id,
    se.partition_label,
    se.park_id,
    se.dose_code,
    se.protocol_name,
    c.tag,
    ''
  FROM sop_task_scan_captures c
  JOIN sop_tasks st ON st.tenant_id = c.tenant_id AND st.task_id = c.task_id
  JOIN goat_places se ON se.goat_id = c.goat_id
  WHERE c.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND (c.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date

  UNION ALL

  SELECT
    'scan_attempt:' || a.attempt_id::text,
    a.captured_at,
    CASE WHEN a.outcome = 'duplicate' THEN 'scan_duplicate' ELSE 'scan_unknown' END,
    a.captured_by,
    se.goat_id,
    se.shed_id,
    se.partition_label,
    se.park_id,
    se.dose_code,
    se.protocol_name,
    a.tag,
    COALESCE(a.reason, a.outcome)
  FROM sop_task_scan_attempts a
  JOIN sop_tasks st ON st.tenant_id = a.tenant_id AND st.task_id = a.task_id
  JOIN goat_places se ON se.goat_id = a.goat_id
  WHERE a.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND a.outcome IN ('duplicate', 'not_due', 'unknown')
    AND (a.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date

  UNION ALL

  -- An unknown tag resolves to NO animal, so it carries no shed, park or vaccine. It is therefore
  -- only included when no shed / partition / park / vaccine narrowing is active; the operator axis
  -- still applies, because captured_by is known. Showing it under a shed filter would break the
  -- page's "filters apply to every section" contract.
  SELECT
    'scan_attempt:' || a.attempt_id::text,
    a.captured_at,
    'scan_unknown',
    a.captured_by,
    NULL::uuid,
    NULL::uuid,
    ''::text,
    NULL::uuid,
    ''::text,
    ''::text,
    a.tag,
    COALESCE(a.reason, a.outcome)
  FROM sop_task_scan_attempts a
  JOIN sop_tasks st ON st.tenant_id = a.tenant_id AND st.task_id = a.task_id
  WHERE a.tenant_id = $1::uuid
    AND st.task_type = 'vaccination'
    AND a.goat_id IS NULL
    AND a.outcome IN ('not_due', 'unknown')
    AND $3::text = '' AND $4::text = '' AND $5::text = '' AND $7::text = ''
    AND ($6::text = '' OR a.captured_by IN (
      SELECT wm.user_id FROM workforce_members wm
      WHERE wm.tenant_id = $1::uuid AND wm.workforce_member_id::text = $6::text
    ))
    AND (a.captured_at AT TIME ZONE '` + istZone + `')::date = $2::date

  UNION ALL

  SELECT
    'administration:' || vc.completion_id::text,
    vc.administered_at,
    'administration',
    vc.recorded_by,
    se.goat_id,
    se.shed_id,
    se.partition_label,
    se.park_id,
    se.dose_code,
    se.protocol_name,
    ''::text,
    ''::text
  FROM vaccination_completions vc
  JOIN scoped_enriched sc ON sc.obligation_id = vc.obligation_id
  JOIN shed_names se ON se.goat_id = sc.goat_id AND se.dose_code = sc.dose_code
  WHERE vc.tenant_id = $1::uuid
    AND vc.status <> 'reversed'
    AND (vc.administered_at AT TIME ZONE '` + istZone + `')::date = $2::date

  UNION ALL

  SELECT
    'shed_submitted:' || ose.obligation_event_id::text,
    ose.occurred_at,
    'shed_submitted',
    ose.actor_id,
    se.goat_id,
    se.shed_id,
    se.partition_label,
    se.park_id,
    se.dose_code,
    se.protocol_name,
    ''::text,
    ''::text
  FROM obligation_status_events ose
  JOIN scoped_enriched sc ON sc.obligation_id = ose.obligation_id
  JOIN shed_names se ON se.goat_id = sc.goat_id AND se.dose_code = sc.dose_code
  WHERE ose.tenant_id = $1::uuid
    AND ose.event_type = 'completed'
    AND (ose.occurred_at AT TIME ZONE '` + istZone + `')::date = $2::date
)
SELECT
  e.event_id,
  e.occurred_at,
  e.kind,
  COALESCE(e.actor_id::text, ''),
  COALESCE(wm.display_name, ''),
  COALESCE(pk.name, ''),
  COALESCE(sh.name, ''),
  e.partition_label,
  e.dose_code,
  e.protocol_name,
  COALESCE(gt.goat_id::text, ''),
  COALESCE(gt.display_id, ''),
  COALESCE(NULLIF(e.scanned_identifier, ''), COALESCE(ident.primary_tag, '')) AS scanned_identifier,
  e.detail_code
FROM events e
-- Same partial-uniqueness trap as the actor query: only the ACTIVE workforce row is unique per
-- user_id, so a bare join duplicates every feed row belonging to a re-hired person.
LEFT JOIN LATERAL (
  SELECT m.display_name
  FROM workforce_members m
  WHERE m.tenant_id = $1::uuid AND m.user_id = e.actor_id AND m.status = 'active'
  ORDER BY m.workforce_member_id
  LIMIT 1
) wm ON true
LEFT JOIN locations pk ON pk.tenant_id = $1::uuid AND pk.location_id = e.park_id
LEFT JOIN locations sh ON sh.tenant_id = $1::uuid AND sh.location_id = e.shed_id
LEFT JOIN goats gt ON gt.tenant_id = $1::uuid AND gt.goat_id = e.goat_id
LEFT JOIN LATERAL (
  SELECT max(gi.identifier_value) AS primary_tag
  FROM goat_identifiers gi
  WHERE gi.tenant_id = $1::uuid
    AND gi.goat_id = e.goat_id
    AND gi.status = 'active'
    AND gi.is_primary_for_goat
) ident ON true
-- Keyset on the FULL sort key (occurred_at, event_id), not on occurred_at alone. A strict
-- timestamp comparison skips every event tied with the previous page's last row, and burst-written
-- scan captures and attempts share a timestamp routinely — that is lost events, not repeated ones.
WHERE ($9::timestamptz IS NULL
    OR e.occurred_at < $9::timestamptz
    OR (e.occurred_at = $9::timestamptz AND ($10::text = '' OR e.event_id < $10::text)))
ORDER BY e.occurred_at DESC, e.event_id DESC
LIMIT $8::int`

// liveTrackerFilterOptionsSQL compiles the filter bar's vocabulary out of the day's OWN rows, with
// only the backend-clamped park scope applied. It deliberately ignores the shed / operator / vaccine
// / status narrowing so that choosing one operator does not collapse the operator list to that one
// operator. Because the vocabulary comes from real rows, an option that matches zero administrations
// cannot be offered — the mock's "FMD + HS (combo)" dead option is structurally impossible here.
var liveTrackerFilterOptionsSQL = liveTrackerScopedCTE + `
SELECT
  'park' AS kind,
  se.park_id::text AS id,
  '' AS code,
  '' AS partition_label,
  COALESCE(pk.name, '') AS label
FROM scoped_enriched se
LEFT JOIN locations pk ON pk.tenant_id = $1::uuid AND pk.location_id = se.park_id
GROUP BY se.park_id, pk.name

UNION ALL

-- chr(31) is the real UNIT SEPARATOR. Spelling it as a backslash-x escape inside a
-- standard-conforming Postgres string literal produces four ordinary characters instead, and
-- nothing errors: the Go side simply never finds its delimiter and every vaccine and shed option
-- renders its raw internal tokens straight into the filter bar.
SELECT 'vaccine', '', se.vaccine_family, '', min(se.protocol_name) || chr(31) || min(se.dose_code)
FROM scoped_enriched se
GROUP BY se.vaccine_family

UNION ALL

SELECT 'operator', se.operator_id::text, '', '', COALESCE(wm.display_name, '')
FROM scoped_enriched se
JOIN workforce_members wm ON wm.tenant_id = $1::uuid AND wm.workforce_member_id = se.operator_id
WHERE se.operator_id IS NOT NULL
GROUP BY se.operator_id, wm.display_name

UNION ALL

SELECT 'shed', se.shed_id::text, '', se.part_norm, COALESCE(sh.name, '') || chr(31) || se.partition_label
FROM scoped_enriched se
LEFT JOIN locations sh ON sh.tenant_id = $1::uuid AND sh.location_id = se.shed_id
GROUP BY se.shed_id, sh.name, se.part_norm, se.partition_label
ORDER BY 1, 5
LIMIT ` + fmt.Sprint(domain.LiveTrackerMaxFilterOptions)

// liveTrackerVerificationSQL is the post-drive verification block. Pending is the standing queue (it
// is not day-scoped: an item captured yesterday is still awaiting review today); verified and rework
// are day-scoped through verified_at.
const liveTrackerVerificationSQL = `
-- projection-review: membership=verification_items for one tenant whose module or source_module is vaccination, optionally narrowed to one park/shed; group_key=none (five scalar aggregates); join_cardinality=no joins, single-table scan; pagination=not applicable, aggregates only, no row list is returned; scope=tenant plus the backend-clamped park filter and the optional shed filter.
SELECT
  count(*) FILTER (WHERE vi.status = 'pending')::int,
  count(DISTINCT vi.shed_id) FILTER (WHERE vi.status = 'pending')::int,
  count(*) FILTER (WHERE vi.status = 'approved' AND (vi.verified_at AT TIME ZONE '` + istZone + `')::date = $2::date)::int,
  count(DISTINCT vi.shed_id) FILTER (WHERE vi.status = 'approved' AND (vi.verified_at AT TIME ZONE '` + istZone + `')::date = $2::date)::int,
  -- Day-scoped through verified_at, exactly like the two 'approved' counters above. Without the date
  -- predicate this was an all-time, tenant-wide rejected total rendered directly beneath a
  -- today-only "Verified today" figure, on a page headed "Drive Day — <date>".
  count(*) FILTER (WHERE vi.status = 'rejected' AND (vi.verified_at AT TIME ZONE '` + istZone + `')::date = $2::date)::int
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND (vi.module = 'vaccination' OR vi.source_module = 'vaccination')
  AND ($3::text = '' OR vi.park_id::text = $3::text)
  AND ($4::text = '' OR vi.shed_id::text = $4::text)`

// liveTrackerCell is one park × shed × partition × vaccine × operator rollup row.
type liveTrackerCell struct {
	parkID         string
	parkName       string
	parkCode       string
	shedID         string
	shedName       string
	partitionLabel string
	partNorm       string
	vaccineFamily  string
	doseCode       string
	protocolName   string
	operatorID     string
	operatorName   string
	operatorCode   string
	scheduled      int
	proofed        int
	uploading      int
	scanned        int
	extraAttempts  int
	lastProofAt    *time.Time
	lastActivityAt *time.Time
}

type liveTrackerActor struct {
	memberID       string
	userID         string
	displayName    string
	displayCode    string
	videos         int
	scans          int
	lastActivityAt *time.Time
}

// LiveTracker serves the whole /vaccination/live-tracker page from one canonical read: KPI tiles,
// the operator board, the shed × partition board, the combo-dose card, the live activity feed, the
// attention list, the verification block, and the filter vocabulary.
func (r *Repository) LiveTracker(ctx context.Context, q domain.LiveTrackerQuery) (domain.LiveTrackerResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	loc := q.BusinessDate.Location()
	businessDate := q.BusinessDate.Format("2006-01-02")
	parkFilter := liveTrackerParkFilter(ctx, q)
	shedFilter := optStr(q.ShedID)
	partitionFilter := ""
	if q.PartitionLabel != nil {
		partitionFilter = domain.NormalizePartitionLabel(*q.PartitionLabel)
	}
	operatorFilter := optStr(q.OperatorID)
	vaccineFilter := strings.ToLower(strings.TrimSpace(optStr(q.VaccineCode)))
	activityLimit := q.ActivityLimit
	if activityLimit <= 0 {
		activityLimit = domain.LiveTrackerDefaultActivity
	}
	if activityLimit > domain.LiveTrackerMaxActivity {
		activityLimit = domain.LiveTrackerMaxActivity
	}

	base := []any{q.TenantID, businessDate, parkFilter, shedFilter, partitionFilter, operatorFilter, vaccineFilter}

	cells, err := r.liveTrackerCells(ctx, base)
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker cells: %w", err)
	}
	actors, err := r.liveTrackerActors(ctx, base)
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker actors: %w", err)
	}
	combo, err := r.liveTrackerCombo(ctx, base)
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker combo: %w", err)
	}
	activity, err := r.liveTrackerActivity(ctx, base, activityLimit, q.ActivityBefore, optStr(q.ActivityBeforeID))
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker activity: %w", err)
	}
	// The filter vocabulary is compiled under the AUTHORIZATION clamp, never under the user's own park
	// selection. Passing the selected park here made the park control self-collapsing: once a park was
	// chosen the dropdown offered only that park and the user could not switch back, and on a park
	// with no administrations that day the list came back empty and the active chip fell through to
	// rendering a raw park UUID. The other four controls already deliberately ignore the narrowing.
	options, err := r.liveTrackerFilterOptions(ctx, q.TenantID, businessDate, liveTrackerAuthorizedParkClamp(ctx, q.TenantID))
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker filter options: %w", err)
	}
	verification, err := r.liveTrackerVerification(ctx, q.TenantID, businessDate, parkFilter, shedFilter)
	if err != nil {
		return domain.LiveTrackerResponse{}, fmt.Errorf("live tracker verification: %w", err)
	}

	now := time.Now().In(loc)
	// Row state (slow shed / idle operator) is elapsed-time arithmetic, and the handler accepts a
	// business_date up to LiveTrackerBusinessDateLookbackDays back. Measuring a PAST drive day against
	// wall-clock now puts every elapsed figure in the thousands of minutes: every unfinished shed
	// becomes `slow`, every operator with work left becomes `idle`, and the Attention list fills with
	// manufactured rows for a drive that closed days ago. The state clock is therefore the earlier of
	// now and the end of the requested drive day.
	stateClock := liveTrackerStateClock(q.BusinessDate, loc, now)
	sheds := liveTrackerShedRows(cells, stateClock)
	operators := liveTrackerOperatorRows(cells, actors, stateClock)

	// The status filter narrows the TILES as well as the tables. sheds[i] is built 1:1 from cells[i],
	// so the surviving cells are exactly the administrations behind the surviving shed rows — which is
	// what keeps "Scheduled" equal to the sum of the shed table's Scheduled column under every filter.
	// Leaving the tiles unfiltered here is precisely the defect the mock shipped: a headline that no
	// longer described the rows beneath it, with no way to tell which number was wrong.
	kpiCells := cells
	if q.Status != nil {
		kept := make([]liveTrackerCell, 0, len(cells))
		survivors := make([]domain.LiveTrackerShedRow, 0, len(sheds))
		for i, row := range sheds {
			if domain.ShedMatchesStatus(row.State, *q.Status) {
				survivors = append(survivors, row)
				kept = append(kept, cells[i])
			}
		}
		sheds = survivors
		kpiCells = kept
		operators = filterOperatorRows(operators, *q.Status)
	}

	// Attention is derived AFTER the status filter, from the sheds and operators that actually
	// survived it. Deriving it first left the Attention list and the Attention tile describing the
	// whole day while the two tables beneath them described a subset — the tiles-vs-tables divergence
	// this whole single-CTE design exists to prevent.
	attention := liveTrackerAttention(sheds, operators, stateClock)

	// Truncation is reported, never silent. Past these caps the tiles (folded from kpiCells, which is
	// NOT truncated) legitimately read higher than the visible table sums, and the response says so.
	operatorsTotal := len(operators)
	shedsTotal := len(sheds)
	if len(operators) > domain.LiveTrackerMaxOperators {
		operators = operators[:domain.LiveTrackerMaxOperators]
	}
	if len(sheds) > domain.LiveTrackerMaxSheds {
		sheds = sheds[:domain.LiveTrackerMaxSheds]
	}

	kpis := liveTrackerKPIs(kpiCells, combo.AnimalCount, len(attention))
	return domain.LiveTrackerResponse{
		BusinessDate:       businessDate,
		GeneratedAt:        now,
		KPIs:               kpis,
		Operators:          operators,
		Sheds:              sheds,
		Combo:              combo,
		OperatorsTotal:     operatorsTotal,
		OperatorsTruncated: operatorsTotal > len(operators),
		ShedsTotal:         shedsTotal,
		ShedsTruncated:     shedsTotal > len(sheds),
		CellsTruncated:     len(cells) >= domain.LiveTrackerMaxCells,
		Activity:           activity,
		Attention:          attention,
		Verification:       verification,
		FilterOptions:      options,
	}, nil
}

// liveTrackerStateClock is the clock every elapsed-minutes decision on this page is measured
// against. For today's drive it is wall-clock now; for a past drive day it is that day's closing
// instant, because "idle for 4300 minutes" is not an observation about a finished drive.
func liveTrackerStateClock(businessDate time.Time, loc *time.Location, now time.Time) time.Time {
	dayEnd := time.Date(businessDate.Year(), businessDate.Month(), businessDate.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	if now.After(dayEnd) {
		return dayEnd
	}
	return now
}

// liveTrackerAuthorizedParkClamp is the park narrowing that authorization forces, independent of any
// park the caller selected. The filter bar's own vocabulary is compiled under THIS clamp so the park
// control cannot collapse to the one park already chosen.
func liveTrackerAuthorizedParkClamp(ctx context.Context, tenantID string) string {
	if parks := authorizedParkFilter(ctx, tenantID); len(parks) == 1 {
		return parks[0]
	}
	return ""
}

// liveTrackerParkFilter narrows the read to one park. The HTTP handler has already clamped the
// request through authorizedParkID; this adds the defence-in-depth in-query narrowing every other
// read in this adapter applies, so a park-scoped actor who omits park_id still cannot read the other
// park's drive.
func liveTrackerParkFilter(ctx context.Context, q domain.LiveTrackerQuery) string {
	if requested := optStr(q.ParkID); requested != "" {
		return requested
	}
	parks := authorizedParkFilter(ctx, q.TenantID)
	if len(parks) == 1 {
		return parks[0]
	}
	return ""
}

func (r *Repository) liveTrackerCells(ctx context.Context, base []any) ([]liveTrackerCell, error) {
	rows, err := r.pool.Query(ctx, liveTrackerCellsSQL, base...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]liveTrackerCell, 0, 64)
	for rows.Next() {
		var c liveTrackerCell
		if err := rows.Scan(&c.parkID, &c.parkName, &c.parkCode, &c.shedID, &c.shedName, &c.partitionLabel,
			&c.partNorm, &c.vaccineFamily, &c.doseCode, &c.protocolName, &c.operatorID, &c.operatorName,
			&c.operatorCode, &c.scheduled, &c.proofed, &c.uploading, &c.scanned, &c.extraAttempts,
			&c.lastProofAt, &c.lastActivityAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) liveTrackerActors(ctx context.Context, base []any) ([]liveTrackerActor, error) {
	rows, err := r.pool.Query(ctx, liveTrackerActorSQL, base...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]liveTrackerActor, 0, 16)
	for rows.Next() {
		var a liveTrackerActor
		if err := rows.Scan(&a.memberID, &a.userID, &a.displayName, &a.displayCode, &a.videos, &a.scans, &a.lastActivityAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) liveTrackerCombo(ctx context.Context, base []any) (domain.LiveTrackerCombo, error) {
	args := append(append([]any{}, base...), domain.LiveTrackerMaxComboRows)
	rows, err := r.pool.Query(ctx, liveTrackerComboSQL, args...)
	if err != nil {
		return domain.LiveTrackerCombo{}, err
	}
	defer rows.Close()

	combo := domain.LiveTrackerCombo{Rows: []domain.LiveTrackerComboRow{}, VaccineLabels: []string{}}
	byGoat := map[string]int{}
	labelSeen := map[string]bool{}
	for rows.Next() {
		var (
			animalCount                                  int
			goatID, displayID, primaryTag, secondaryTag  string
			shedName, partitionLabel                     string
			obligationID, family, doseCode, protocolName string
			status                                       string
			proofCompletedCount, proofPendingCount       int
		)
		if err := rows.Scan(&animalCount, &goatID, &displayID, &primaryTag, &secondaryTag, &shedName,
			&partitionLabel, &obligationID, &family, &doseCode, &protocolName, &status,
			&proofCompletedCount, &proofPendingCount); err != nil {
			return domain.LiveTrackerCombo{}, err
		}
		combo.AnimalCount = animalCount
		idx, ok := byGoat[goatID]
		if !ok {
			combo.Rows = append(combo.Rows, domain.LiveTrackerComboRow{
				GoatID:       goatID,
				DisplayID:    displayID,
				PrimaryTag:   primaryTag,
				SecondaryTag: secondaryTag,
				ShedLabel:    domain.ShedDisplayLabel(shedName, partitionLabel),
				ProofState:   liveTrackerProofState(proofCompletedCount, proofPendingCount),
				ProofCount:   proofCompletedCount + proofPendingCount,
				Doses:        []domain.LiveTrackerComboDose{},
			})
			idx = len(combo.Rows) - 1
			byGoat[goatID] = idx
		}
		label := vaccinatdomain.DoseDisplayLabel(protocolName, doseCode)
		if !labelSeen[label] {
			labelSeen[label] = true
			combo.VaccineLabels = append(combo.VaccineLabels, label)
		}
		combo.Rows[idx].Doses = append(combo.Rows[idx].Doses, domain.LiveTrackerComboDose{
			ObligationID: obligationID,
			VaccineCode:  family,
			VaccineLabel: label,
			State:        liveTrackerDoseState(status, proofCompletedCount),
		})
	}
	if err := rows.Err(); err != nil {
		return domain.LiveTrackerCombo{}, err
	}
	sort.Strings(combo.VaccineLabels)
	combo.RowsTruncated = combo.AnimalCount > len(combo.Rows)
	return combo, nil
}

func (r *Repository) liveTrackerActivity(ctx context.Context, base []any, limit int, before *time.Time, beforeID string) (domain.LiveTrackerActivity, error) {
	var beforeArg any
	if before != nil {
		beforeArg = *before
	}
	args := append(append([]any{}, base...), limit, beforeArg, beforeID)
	rows, err := r.pool.Query(ctx, liveTrackerActivitySQL, args...)
	if err != nil {
		return domain.LiveTrackerActivity{}, err
	}
	defer rows.Close()

	out := domain.LiveTrackerActivity{Items: []domain.LiveTrackerActivityItem{}}
	for rows.Next() {
		var (
			item                   domain.LiveTrackerActivityItem
			partitionLabel         string
			shedName               string
			doseCode, protocolName string
		)
		if err := rows.Scan(&item.EventID, &item.OccurredAt, &item.Kind, &item.ActorID, &item.ActorName,
			&item.ParkName, &shedName, &partitionLabel, &doseCode, &protocolName, &item.GoatID,
			&item.GoatDisplayID, &item.ScannedIdentifier, &item.DetailCode); err != nil {
			return domain.LiveTrackerActivity{}, err
		}
		item.ShedLabel = domain.ShedDisplayLabel(shedName, partitionLabel)
		if doseCode != "" || protocolName != "" {
			item.VaccineLabel = vaccinatdomain.DoseDisplayLabel(protocolName, doseCode)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.LiveTrackerActivity{}, err
	}
	if len(out.Items) == limit && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1]
		cursor := last.OccurredAt
		eventID := last.EventID
		out.NextCursor = &cursor
		// Both halves, always. A caller handed only the timestamp cannot page without losing ties.
		out.NextCursorEventID = &eventID
	}
	// The feed rate is OBSERVED over the returned window, never assumed. Fewer than two events give
	// no window at all, so the rate is reported as absent rather than invented.
	if len(out.Items) >= 2 {
		newest := out.Items[0].OccurredAt
		oldest := out.Items[len(out.Items)-1].OccurredAt
		minutes := newest.Sub(oldest).Minutes()
		out.WindowMinutes = int(math.Round(minutes))
		if minutes > 0 {
			rate := math.Round(float64(len(out.Items))/minutes*10) / 10
			out.ObservedPerMin = &rate
		}
	}
	return out, nil
}

func (r *Repository) liveTrackerFilterOptions(ctx context.Context, tenantID, businessDate, parkFilter string) (domain.LiveTrackerFilterOptions, error) {
	rows, err := r.pool.Query(ctx, liveTrackerFilterOptionsSQL, tenantID, businessDate, parkFilter, "", "", "", "")
	if err != nil {
		return domain.LiveTrackerFilterOptions{}, err
	}
	defer rows.Close()

	out := domain.LiveTrackerFilterOptions{
		Parks:     []domain.LiveTrackerFilterOption{},
		Vaccines:  []domain.LiveTrackerFilterOption{},
		Operators: []domain.LiveTrackerFilterOption{},
		Sheds:     []domain.LiveTrackerFilterOption{},
	}
	for rows.Next() {
		var kind, id, code, partition, label string
		if err := rows.Scan(&kind, &id, &code, &partition, &label); err != nil {
			return domain.LiveTrackerFilterOptions{}, err
		}
		switch kind {
		case "park":
			out.Parks = append(out.Parks, domain.LiveTrackerFilterOption{ID: id, Label: label})
		case "vaccine":
			protocolName, doseCode, _ := strings.Cut(label, "\x1f")
			out.Vaccines = append(out.Vaccines, domain.LiveTrackerFilterOption{
				Code:  code,
				Label: vaccinatdomain.DoseDisplayLabel(protocolName, doseCode),
			})
		case "operator":
			out.Operators = append(out.Operators, domain.LiveTrackerFilterOption{ID: id, Label: label})
		case "shed":
			shedName, partitionLabel, _ := strings.Cut(label, "\x1f")
			out.Sheds = append(out.Sheds, domain.LiveTrackerFilterOption{
				ID:             id,
				PartitionLabel: partition,
				Label:          domain.ShedDisplayLabel(shedName, partitionLabel),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return domain.LiveTrackerFilterOptions{}, err
	}
	sortOptions(out.Parks)
	sortOptions(out.Vaccines)
	sortOptions(out.Operators)
	sortOptions(out.Sheds)
	return out, nil
}

func (r *Repository) liveTrackerVerification(ctx context.Context, tenantID, businessDate, parkFilter, shedFilter string) (domain.LiveTrackerVerification, error) {
	var v domain.LiveTrackerVerification
	err := r.pool.QueryRow(ctx, liveTrackerVerificationSQL, tenantID, businessDate, parkFilter, shedFilter).
		Scan(&v.AwaitingReviewItems, &v.AwaitingReviewSheds, &v.VerifiedTodayItems, &v.VerifiedTodaySheds, &v.ReworkRequested)
	if err != nil && err != pgx.ErrNoRows {
		return domain.LiveTrackerVerification{}, err
	}
	return v, nil
}

func sortOptions(opts []domain.LiveTrackerFilterOption) {
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
}

func liveTrackerProofState(completed, pending int) string {
	switch {
	case completed > 0:
		return domain.LiveTrackerProofVideo
	case pending > 0:
		return domain.LiveTrackerProofUploading
	default:
		return domain.LiveTrackerProofNone
	}
}

func liveTrackerDoseState(status string, completedProofs int) string {
	switch status {
	case "completed":
		return domain.LiveTrackerDoseClosed
	case "in_progress", "due":
		if completedProofs > 0 {
			return domain.LiveTrackerDoseClosed
		}
		return domain.LiveTrackerDoseAwaitingProof
	default:
		if completedProofs > 0 {
			return domain.LiveTrackerDoseClosed
		}
		return domain.LiveTrackerDoseScheduled
	}
}

// liveTrackerShedRows folds the cell rollup into the shed × partition board.
func liveTrackerShedRows(cells []liveTrackerCell, now time.Time) []domain.LiveTrackerShedRow {
	out := make([]domain.LiveTrackerShedRow, 0, len(cells))
	for _, c := range cells {
		remaining := c.scheduled - c.proofed
		if remaining < 0 {
			remaining = 0
		}
		row := domain.LiveTrackerShedRow{
			ShedID:              c.shedID,
			ShedName:            c.shedName,
			PhysicalShed:        c.shedName,
			PartitionLabel:      c.partitionLabel,
			ShedLabel:           domain.ShedDisplayLabel(c.shedName, c.partitionLabel),
			ParkID:              c.parkID,
			ParkName:            c.parkName,
			VaccineCode:         c.vaccineFamily,
			VaccineLabel:        vaccinatdomain.DoseDisplayLabel(c.protocolName, c.doseCode),
			OperatorID:          c.operatorID,
			OperatorName:        c.operatorName,
			ScheduledAdmins:     c.scheduled,
			ProofVideosReceived: c.proofed,
			Remaining:           remaining,
			LastProofAt:         c.lastProofAt,
			ExtraAttemptCount:   c.extraAttempts,
		}
		row.State = liveTrackerShedState(row, c.lastActivityAt, now)
		out = append(out, row)
	}
	return out
}

func liveTrackerShedState(row domain.LiveTrackerShedRow, lastActivityAt *time.Time, now time.Time) string {
	switch {
	case row.Remaining == 0 && row.ExtraAttemptCount > 0:
		return domain.LiveTrackerShedReview
	case row.Remaining == 0:
		return domain.LiveTrackerShedDone
	case row.ProofVideosReceived == 0:
		return domain.LiveTrackerShedNotStarted
	}
	ratio := 0.0
	if row.ScheduledAdmins > 0 {
		ratio = float64(row.ProofVideosReceived) / float64(row.ScheduledAdmins)
	}
	openMinutes := 0.0
	if lastActivityAt != nil {
		openMinutes = now.Sub(*lastActivityAt).Minutes()
	}
	if ratio < domain.LiveTrackerSlowShedRatio && openMinutes > domain.LiveTrackerSlowShedMinutes {
		return domain.LiveTrackerShedSlow
	}
	return domain.LiveTrackerShedReceiving
}

// liveTrackerOperatorRows folds the cell rollup (which carries the operator's ASSIGNED workload and
// current location) together with the actor rollup (which carries the evidence they personally
// produced). An operator who acted without an assignment still gets a row rather than vanishing.
func liveTrackerOperatorRows(cells []liveTrackerCell, actors []liveTrackerActor, now time.Time) []domain.LiveTrackerOperatorRow {
	type acc struct {
		row      domain.LiveTrackerOperatorRow
		bestSeen *time.Time
	}
	byOperator := map[string]*acc{}
	order := make([]string, 0, len(cells))

	for _, c := range cells {
		if c.operatorID == "" {
			continue
		}
		entry, ok := byOperator[c.operatorID]
		if !ok {
			entry = &acc{row: domain.LiveTrackerOperatorRow{
				OperatorID:          c.operatorID,
				OperatorName:        c.operatorName,
				OperatorDisplayCode: c.operatorCode,
				IdentityResolved:    c.operatorCode != "" && !strings.HasPrefix(c.operatorCode, "auth:"),
				ParkID:              c.parkID,
				ParkName:            c.parkName,
				ParkCode:            c.parkCode,
			}}
			byOperator[c.operatorID] = entry
			order = append(order, c.operatorID)
		}
		entry.row.ScheduledAdmins += c.scheduled
		// "Now at" is the cell this operator most recently produced evidence in; with no evidence yet
		// it stays on the first assigned cell so the row still names where the work is.
		if entry.row.CurrentShedID == "" || (c.lastActivityAt != nil && (entry.bestSeen == nil || c.lastActivityAt.After(*entry.bestSeen))) {
			entry.row.CurrentShedID = c.shedID
			entry.row.CurrentShedLabel = domain.ShedDisplayLabel(c.shedName, c.partitionLabel)
			entry.row.CurrentPartitionLabel = c.partitionLabel
			entry.row.CurrentVaccineLabel = vaccinatdomain.DoseDisplayLabel(c.protocolName, c.doseCode)
			if c.lastActivityAt != nil {
				entry.bestSeen = c.lastActivityAt
			}
		}
	}

	byMember := map[string]*liveTrackerActor{}
	for i := range actors {
		a := &actors[i]
		if a.memberID != "" {
			byMember[a.memberID] = a
		}
	}
	for id, entry := range byOperator {
		if a, ok := byMember[id]; ok {
			entry.row.ProofVideos = a.videos
			entry.row.ScanCaptures = a.scans
			entry.row.LastActivityAt = a.lastActivityAt
			if entry.row.OperatorName == "" {
				entry.row.OperatorName = a.displayName
			}
		}
	}
	// An operator who produced evidence but holds no assignment row for this day is still on the
	// drive; dropping them would under-report the board against the feed showing their events.
	for i := range actors {
		a := &actors[i]
		if a.memberID == "" {
			continue
		}
		if _, ok := byOperator[a.memberID]; ok {
			continue
		}
		byOperator[a.memberID] = &acc{row: domain.LiveTrackerOperatorRow{
			OperatorID:          a.memberID,
			OperatorName:        a.displayName,
			OperatorDisplayCode: a.displayCode,
			IdentityResolved:    a.displayCode != "" && !strings.HasPrefix(a.displayCode, "auth:"),
			ProofVideos:         a.videos,
			ScanCaptures:        a.scans,
			LastActivityAt:      a.lastActivityAt,
		}}
		order = append(order, a.memberID)
	}

	out := make([]domain.LiveTrackerOperatorRow, 0, len(order))
	for _, id := range order {
		entry := byOperator[id]
		row := entry.row
		row.Remaining = row.ScheduledAdmins - row.ProofVideos
		if row.Remaining < 0 {
			row.Remaining = 0
		}
		if row.LastActivityAt != nil {
			idle := int(now.Sub(*row.LastActivityAt).Minutes())
			if idle < 0 {
				idle = 0
			}
			row.IdleMinutes = &idle
		}
		row.State = liveTrackerOperatorState(row)
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ParkName != out[j].ParkName {
			return out[i].ParkName < out[j].ParkName
		}
		return out[i].OperatorName < out[j].OperatorName
	})
	return out
}

func liveTrackerOperatorState(row domain.LiveTrackerOperatorRow) string {
	if row.ProofVideos == 0 && row.ScanCaptures == 0 {
		return domain.LiveTrackerOperatorNotStarted
	}
	if row.Remaining == 0 && row.ScheduledAdmins > 0 {
		return domain.LiveTrackerOperatorDone
	}
	if row.IdleMinutes != nil && *row.IdleMinutes >= domain.LiveTrackerIdleMinutes && row.Remaining > 0 {
		return domain.LiveTrackerOperatorIdle
	}
	return domain.LiveTrackerOperatorActive
}

// liveTrackerAttention emits one row per real condition found in the SAME rollups the tables render,
// so the Attention tile equals len(attention) by construction rather than by a hardcoded number.
//
// ElapsedMin is always a MEASURED gap — minutes since the subject's last real evidence — never a
// policy threshold. Emitting domain.LiveTrackerSlowShedMinutes here rendered the constant 40 on
// screen as if it were an observation of that shed.
func liveTrackerAttention(sheds []domain.LiveTrackerShedRow, operators []domain.LiveTrackerOperatorRow, stateClock time.Time) []domain.LiveTrackerAttentionRow {
	out := make([]domain.LiveTrackerAttentionRow, 0, 8)
	for _, op := range operators {
		if op.State != domain.LiveTrackerOperatorIdle {
			continue
		}
		row := domain.LiveTrackerAttentionRow{
			Kind:         domain.LiveTrackerAttentionIdleOperator,
			SubjectLabel: op.OperatorName,
			OperatorID:   op.OperatorID,
			MetricCount:  op.ProofVideos,
			TotalCount:   op.ScheduledAdmins,
			SinceAt:      op.LastActivityAt,
			Severity:     "dng",
		}
		if op.IdleMinutes != nil {
			row.ElapsedMin = *op.IdleMinutes
		}
		out = append(out, row)
	}
	for _, shed := range sheds {
		if shed.ExtraAttemptCount > 0 {
			out = append(out, domain.LiveTrackerAttentionRow{
				Kind:         domain.LiveTrackerAttentionExtraAttempts,
				SubjectLabel: shed.ShedLabel,
				ShedID:       shed.ShedID,
				MetricCount:  shed.ExtraAttemptCount,
				TotalCount:   shed.ScheduledAdmins,
				SinceAt:      shed.LastProofAt,
				Severity:     "warn",
			})
		}
		if shed.State == domain.LiveTrackerShedSlow {
			row := domain.LiveTrackerAttentionRow{
				Kind:         domain.LiveTrackerAttentionSlowShed,
				SubjectLabel: shed.ShedLabel,
				ShedID:       shed.ShedID,
				MetricCount:  shed.ProofVideosReceived,
				TotalCount:   shed.ScheduledAdmins,
				SinceAt:      shed.LastProofAt,
				Severity:     "warn",
			}
			// Measured: minutes since this shed's last completed proof. With no proof at all there is
			// nothing to measure, so the row reports no elapsed figure rather than a stand-in — the
			// "well under a quarter done this far into the drive" explanation already lives in the
			// attention-kind title.
			if shed.LastProofAt != nil {
				if idle := int(stateClock.Sub(*shed.LastProofAt).Minutes()); idle > 0 {
					row.ElapsedMin = idle
				}
			}
			out = append(out, row)
		}
	}
	return out
}

func liveTrackerKPIs(cells []liveTrackerCell, comboAnimals, attentionCount int) domain.LiveTrackerKPIs {
	kpis := domain.LiveTrackerKPIs{ScheduledByPark: []domain.LiveTrackerParkCount{}}
	byPark := map[string]*domain.LiveTrackerParkCount{}
	parkOrder := make([]string, 0, 4)
	activeParks := map[string]bool{}
	for _, c := range cells {
		kpis.ScheduledAdministrations += c.scheduled
		kpis.ProofVideosReceived += c.proofed
		kpis.ScanCaptures += c.scanned
		entry, ok := byPark[c.parkID]
		if !ok {
			entry = &domain.LiveTrackerParkCount{ParkID: c.parkID, ParkName: c.parkName, ParkCode: c.parkCode}
			byPark[c.parkID] = entry
			parkOrder = append(parkOrder, c.parkID)
		}
		entry.Count += c.scheduled
		if c.proofed > 0 || c.scanned > 0 {
			activeParks[c.parkID] = true
		}
	}
	for _, id := range parkOrder {
		kpis.ScheduledByPark = append(kpis.ScheduledByPark, *byPark[id])
	}
	kpis.Remaining = kpis.ScheduledAdministrations - kpis.ProofVideosReceived
	if kpis.Remaining < 0 {
		kpis.Remaining = 0
	}
	kpis.ComboAnimals = comboAnimals
	kpis.AttentionCount = attentionCount
	kpis.ActiveParks = len(activeParks)
	return kpis
}

func filterOperatorRows(rows []domain.LiveTrackerOperatorRow, status domain.LiveTrackerStatus) []domain.LiveTrackerOperatorRow {
	out := make([]domain.LiveTrackerOperatorRow, 0, len(rows))
	for _, row := range rows {
		if domain.OperatorMatchesStatus(row.State, status) {
			out = append(out, row)
		}
	}
	return out
}
