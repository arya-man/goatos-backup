package postgres

// Command-board drilldown SQL: the lists a reader opens FROM a board cell.
//
// Every statement here was previously executed eagerly on GET /vaccination/command, tenant-wide,
// and capped in Go afterwards. Two properties are new and both are load-bearing:
//
//  1. THE CELL IS A PREDICATE, NOT A POST-FILTER. Each list now requires the cell it explains and
//     puts that cell in the WHERE clause, so the database does one drawer's worth of work instead
//     of the whole tenant's. The old shed-vaccine list sorted an estimated 57,176 rows to return
//     500, and on the live tenant spent 2.4s returning zero.
//
//  2. THE PAGE IS APPLIED BEFORE DECORATION. Identifiers, locations, partitions and closure
//     reasons are joined onto the page, never onto the candidate set. The old closed-without-dose
//     statement did the reverse and additionally re-scanned a tenant-wide CTE once per candidate
//     animal through a LATERAL, which is what exhausted the 15s statement timeout in staging.
//
// The bucket predicates are otherwise unchanged and deliberately so: a drilldown that disagreed
// with the tile it hangs off would be worse than a slow one. commandboard_query_plan_test.go
// asserts list-vs-tile agreement against live data.

// commandBoardClosedWithoutDoseSQL lists the animals behind the ClosedWithoutDose tile.
//
// The residual bucket is defined exactly as the tile defines it — an animal none of whose
// obligations is verified, awaiting, overdue or scheduled — using the same comp/scoped/per_animal
// fold commandBoardKPISQL uses. That much is unchanged.
//
// WHAT CHANGED IS THE SHAPE, and it is the fix for the reported 500:
//
//   - `page` selects the residual animals, applies the keyset, and LIMITs, using nothing but
//     goats. Every decorating join below hangs off `page`, so identifier lookups and location
//     joins run at most $7 times instead of once per residual animal in the tenant.
//
//   - `latest_closed` replaces a correlated LATERAL that re-scanned the whole `scoped` CTE for
//     every candidate animal. Postgres materialises `scoped` (~70k rows on the live tenant), so
//     that LATERAL was an N x 70k CTE re-scan, and it is where
//     `closed-without-dose rows: timeout: context deadline exceeded` came from. DISTINCT ON over
//     `scoped` joined to `page` computes the same "most recent closure per animal" set-wise, once,
//     for the page only. Same ORDER BY, so it picks the same row per animal as before.
//
// Keyset: ORDER BY (display_id, goat_id). display_id is not unique, so goat_id makes the order
// total; a keyset over a non-total order drops or repeats rows at page boundaries.
const commandBoardClosedWithoutDoseSQL = `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
scoped AS (
  SELECT
    oi.target_id,
    oi.obligation_id,
    oi.status,
    oi.rule_id,
    oi.due_at,
    COALESCE(comp.has_accepted, false) AS has_accepted,
    COALESCE(comp.has_recorded_unverified, false) AS has_recorded_unverified,
    comp.obligation_id IS NULL AS no_completion,
    oi.status IN ('scheduled','due','in_progress','deferred','missed') AS is_open,
    oi.status = 'missed' AS is_missed,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date AS due_before_as_of
  FROM obligation_instances oi
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
),
per_animal AS (
  SELECT
    target_id,
    -- GATED ON no_completion, exactly as commandBoardKPISQL gates it. This is the predicate drift
    -- the tile/drawer split was reported for: the drawer folded any_missed WITHOUT the
    -- no-completion gate AND then never applied it, so it listed animals the tile does not count.
    -- The prose in the KPI statement additionally claimed any_missed is "deliberately NOT gated on
    -- no_completion" while its SQL gates it -- comment and code disagreed, and the comment was the
    -- wrong one. The SQL is the contract; both statements now spell it identically.
    bool_or(is_missed AND no_completion) AS any_missed,
    bool_or(has_accepted) AS any_verified,
    bool_or(has_recorded_unverified AND NOT has_accepted) AS any_awaiting,
    bool_or(is_open AND no_completion AND due_before_as_of) AS any_overdue,
    bool_or(is_open AND no_completion AND NOT due_before_as_of) AS any_scheduled
  FROM scoped
  GROUP BY target_id
),
-- THE PAGE. Nothing but goats is joined here, so the keyset and the LIMIT are applied before any
-- decorating work happens. Everything below reads from this at most $7 rows wide.
page AS (
  SELECT g.goat_id, g.display_id, g.tenant_id, g.shed_id
  FROM per_animal pa
  JOIN goats g ON g.goat_id = pa.target_id AND g.tenant_id = $1::uuid
  -- BYTE-IDENTICAL to the tile's residual filter in commandBoardKPISQL, any_missed included. A
  -- number a reader can click into is a promise that the list explains THAT number.
  WHERE NOT pa.any_missed AND NOT pa.any_verified AND NOT pa.any_awaiting AND NOT pa.any_overdue AND NOT pa.any_scheduled
    AND ($5::text IS NULL OR (g.display_id, g.goat_id) > ($5::text, $6::uuid))
  ORDER BY g.display_id, g.goat_id
  LIMIT $7
),
-- The most recent closure per animal, SET-BASED over the page. This is the LATERAL that used to
-- re-scan the materialised scoped CTE once per animal.
latest_closed AS (
  SELECT DISTINCT ON (s.target_id)
    s.target_id,
    s.status,
    pr.dose_code
  FROM scoped s
  JOIN page p ON p.goat_id = s.target_id
  JOIN protocol_rules pr ON pr.rule_id = s.rule_id AND pr.tenant_id = $1::uuid
  ORDER BY s.target_id, s.due_at DESC NULLS LAST, s.obligation_id
)
SELECT
  p.goat_id::text,
  p.display_id,
  COALESCE(aid1.identifier_value, '') AS animal_identifier_1,
  COALESCE(aid2.identifier_value, '') AS animal_identifier_2,
  COALESCE(park.name, '') AS park_name,
  COALESCE(shed.name, '') AS shed_name,
  COALESCE(gsp.partition_label, '') AS partition_label,
  COALESCE(closed.status, '') AS reason_status,
  COALESCE(closed.dose_code, '') AS dose_code
FROM page p
LEFT JOIN locations shed ON p.shed_id = shed.location_id AND p.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = p.tenant_id AND gsp.goat_id = p.goat_id AND gsp.shed_id = p.shed_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = p.tenant_id AND gi.goat_id = p.goat_id
    AND gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid1 ON true
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = p.tenant_id AND gi.goat_id = p.goat_id
    AND gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid2 ON true
LEFT JOIN latest_closed closed ON closed.target_id = p.goat_id
ORDER BY p.display_id, p.goat_id
`

// commandBoardShedVaccineAnimalSQL lists the animals behind ONE shed-vaccine cell.
//
// The behind predicate is unchanged from the matrix aggregate it explains: no accepted completion,
// AND (recorded-unverified OR 'missed' OR still open with an IST business due date already past).
// The DECORATION is not unchanged: it is now joined to the page rather than to the candidate set,
// and the park/shed name fallback below is called out where it happens.
//
// $5/$6/$7 (shed, vaccine, partition) are REQUIRED and are the whole point. The eager version
// carried no cell predicate at all: it built every cell's list for the tenant, sorted an estimated
// 57k rows, took the first 500 in due-date order and let Go bucket them. That both cost 2.4s and
// was WRONG as a drilldown — a global 500-row cap silently starved the cells that sorted late, so
// a red cell could show an empty drawer while its animals sat under another shed's rows.
//
// Keyset: ORDER BY (due_at NULLS LAST, goat_id). COALESCE(due_at,'infinity') makes the NULLS LAST
// tail expressible as a plain tuple comparison. The resume is gated on goat_id rather than on the
// timestamp -- see the predicate's own comment for why gating on the timestamp makes the undated
// tail repeat forever rather than page.
const commandBoardShedVaccineAnimalSQL = `
WITH comp AS (
  SELECT obligation_id,
         bool_or(status = 'accepted') AS has_accepted,
         bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
         max(administered_at) FILTER (WHERE status = 'recorded' AND verified_at IS NULL) AS recorded_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
cell AS (
  SELECT DISTINCT ON (d.vaccine_code, g.goat_id)
    CASE
      WHEN scope_sp.shed_id IS NULL OR lower(btrim(COALESCE(scope_gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
      ELSE btrim(scope_sp.partition_label)
    END AS scope_partition_label,
    d.vaccine_code,
    g.goat_id,
    g.display_id,
    g.tenant_id,
    g.shed_id AS current_shed_id,
    oi.status,
    oi.due_at,
    COALESCE(comp.has_recorded_unverified, false) AS awaiting_verification,
    comp.recorded_at
  FROM obligation_instances oi
  JOIN protocol_rule_dimensions d ON d.rule_id = oi.rule_id AND d.tenant_id = oi.tenant_id
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
  LEFT JOIN goat_shed_partitions scope_gsp ON scope_gsp.tenant_id = g.tenant_id AND scope_gsp.goat_id = g.goat_id AND scope_gsp.shed_id = oi.scope_id
  LEFT JOIN shed_partitions scope_sp
    ON scope_sp.tenant_id = oi.tenant_id
   AND scope_sp.shed_id = oi.scope_id
   AND scope_sp.normalized_label = regexp_replace(lower(btrim(COALESCE(scope_gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
   AND scope_sp.status = 'active'
  WHERE oi.tenant_id = $1::uuid
    AND oi.scope_type = 'shed'
    -- THE CELL. Applied here, on an indexed column, before any decoration or sorting.
    AND oi.scope_id = $5::uuid
    AND d.vaccine_code = $6::text
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND d.vaccine_code <> ''
    AND NOT COALESCE(comp.has_accepted, false)
    AND (
      COALESCE(comp.has_recorded_unverified, false)
      OR oi.status = 'missed'
      OR (oi.status IN ('scheduled','due','in_progress','deferred')
          AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
    )
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
  ORDER BY d.vaccine_code, g.goat_id, oi.due_at ASC NULLS LAST
),
page AS (
  SELECT *
  FROM cell
  WHERE scope_partition_label = $7::text
    -- GATED ON $9 (goat_id), NOT on $8 (due_at). due_at is nullable and the order is NULLS LAST, so
    -- a cursor sitting in the undated tail carries a NULL due_at -- and gating the resume on $8
    -- would then take the FIRST-PAGE branch and re-emit that tail from its start, forever. goat_id
    -- is never NULL on a real cursor, so it is the only safe presence test. (An earlier attempt
    -- passed a finite year-36812 timestamp as a stand-in for 'infinity'; 'infinity' compares
    -- greater than it, so every undated row still qualified and the page repeated.)
    AND ($9::uuid IS NULL OR (COALESCE(due_at, 'infinity'::timestamptz), goat_id) > (COALESCE($8::timestamptz, 'infinity'::timestamptz), $9::uuid))
  ORDER BY COALESCE(due_at, 'infinity'::timestamptz), goat_id
  LIMIT $10
)
SELECT
  p.scope_partition_label,
  p.vaccine_code,
  p.goat_id::text,
  p.display_id,
  COALESCE(aid1.identifier_value, '') AS tag1,
  COALESCE(aid2.identifier_value, '') AS tag2,
  p.status,
  p.due_at,
  -- FALLBACK TO THE OBLIGATION'S SCOPE shed/park when the animal has no current shed. Dropping it
  -- rendered an empty location in a drawer whose whole purpose is telling an operator where to
  -- walk. scope_id is pinned to $5 here, so this is one PK lookup, not a re-scan.
  COALESCE(current_park.name, scope_park.name, '') AS park_name,
  COALESCE(current_shed.name, scope_shed.name, '') AS shed_name,
  CASE
    WHEN current_sp.shed_id IS NOT NULL THEN btrim(current_sp.partition_label)
    WHEN lower(btrim(COALESCE(current_gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(current_gsp.partition_label)
  END AS partition_label,
  p.awaiting_verification,
  p.recorded_at
FROM page p
LEFT JOIN locations scope_shed ON scope_shed.location_id = $5::uuid AND scope_shed.tenant_id = p.tenant_id
LEFT JOIN locations scope_park ON scope_park.location_id = scope_shed.parent_location_id AND scope_park.tenant_id = scope_shed.tenant_id
LEFT JOIN locations current_shed ON current_shed.location_id = p.current_shed_id AND current_shed.tenant_id = p.tenant_id
LEFT JOIN locations current_park ON current_park.location_id = current_shed.parent_location_id AND current_park.tenant_id = current_shed.tenant_id
LEFT JOIN goat_shed_partitions current_gsp ON current_gsp.tenant_id = p.tenant_id AND current_gsp.goat_id = p.goat_id AND current_gsp.shed_id = p.current_shed_id
LEFT JOIN shed_partitions current_sp
  ON current_sp.tenant_id = p.tenant_id
 AND current_sp.shed_id = p.current_shed_id
 AND current_sp.normalized_label = regexp_replace(lower(btrim(COALESCE(current_gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
 AND current_sp.status = 'active'
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = p.tenant_id AND gi.goat_id = p.goat_id
    AND gi.status = 'active' AND gi.identifier_type = 'animal_identifier_1'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid1 ON true
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = p.tenant_id AND gi.goat_id = p.goat_id
    AND gi.status = 'active' AND gi.identifier_type = 'animal_identifier_2'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid2 ON true
ORDER BY COALESCE(p.due_at, 'infinity'::timestamptz), p.goat_id
`

// commandBoardShedVideoSQL returns one shed's completed proof videos for the given IST days.
//
// Unchanged except for where it is CALLED FROM. Eagerly, the shed set had to be derived from the
// tenant-wide behind-animal scan first, so this cheap query sat behind a 2.4s one. Opened from a
// cell, the shed and the days are already known.
//
// The field_key predicate EXCLUDES the weighing captures by name rather than requiring the word
// "vaccination". Weighing writes weighing_individual_video and weighing_shed_partition_video --
// explicit, self-describing keys -- while the vaccination shed clip is the generic shed_video. An
// include-list keyed on "vaccination" therefore matched nothing and reported "no video uploaded"
// for 137 doses whose footage was sitting in GCS the whole time.
const commandBoardShedVideoSQL = `
SELECT
  pa.scope_id::text AS shed_id,
  pa.proof_id::text,
  pa.uploaded_at,
  COALESCE(pa.duration_ms, 0)
FROM proof_artifacts pa
WHERE pa.tenant_id = $1::uuid
  AND pa.proof_type = 'video'
  AND pa.upload_state = 'completed'
  AND COALESCE(pa.metadata->>'field_key', '') NOT LIKE 'weighing%'
  AND pa.scope_id = ANY($2::uuid[])
  AND (pa.uploaded_at AT TIME ZONE 'Asia/Kolkata')::date = ANY($3::date[])
  -- The park guard is STATED, not inherited. The shed ids reaching this statement happen to come
  -- from an already-park-scoped page, so a foreign shed yields no days and the query is never
  -- issued -- but that is an incidental invariant in the caller, not a property of this statement.
  -- Proof footage is exactly the kind of row that must not leak across a park boundary because a
  -- future caller passed a shed id from somewhere else.
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = pa.scope_id AND pl.tenant_id = pa.tenant_id AND pl.parent_location_id = $4::uuid
  ))
ORDER BY pa.uploaded_at
`

// commandBoardCohortDaySQL splits ONE cohort cell's verified bucket by administration day.
//
// $4/$5/$6/$7 name the cell. The eager version grouped every cohort cell's days for the tenant on
// every render and threw away all but the cells the reader had open (usually none).
//
// $7 is the cell's dose-code SET, not a single code: the board collapses several dose codes onto
// one displayed vaccine label, so a drilldown asking for one code would under-report the column it
// was opened from.
const commandBoardCohortDaySQL = `
SELECT
  (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date AS administered_date,
  COUNT(DISTINCT g.goat_id)::int AS animal_count
FROM obligation_instances oi
JOIN vaccination_completions vc ON vc.obligation_id = oi.obligation_id AND vc.tenant_id = oi.tenant_id AND vc.status = 'accepted'
JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND vc.administered_at IS NOT NULL
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $2::uuid)
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $3::uuid
  ))
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  -- THE CELL.
  AND COALESCE(park.location_id::text, '') = $4::text
  AND g.management_stage = $5::text
  AND g.sex = $6::text
  AND pr.dose_code = ANY($7::text[])
GROUP BY administered_date
ORDER BY administered_date
`

// commandBoardCohortExceptionListSQL lists ONE cohort cell's dose-sequence exceptions -- the
// animals behind the cell's MissingPriorDoseCount.
//
// It is commandBoardCohortExceptionCTE with the cell filter supplied and a keyset page taken off
// the end, so the drawer and the tile are the SAME set by construction rather than by two
// statements kept in step by comment. See that CTE for why the predicate is set-based.
//
// Keyset: ORDER BY (display_id, goat_id). display_id is not unique, so goat_id makes the order
// total; a keyset over a non-total order drops or repeats rows at page boundaries.
const commandBoardCohortExceptionListSQL = commandBoardCohortExceptionCTE + `,
page AS (
  -- DISTINCT because one animal can be an exception in several dose codes of the same cell (the
  -- board collapses those onto one displayed vaccine label), and the drawer names ANIMALS.
  SELECT DISTINCT e.goat_id, e.display_id, e.tenant_id
  FROM exceptions e
  WHERE ($8::text IS NULL OR (e.display_id, e.goat_id) > ($8::text, $9::uuid))
  ORDER BY e.display_id, e.goat_id
  LIMIT $10
)
SELECT
  p.goat_id::text,
  p.display_id,
  COALESCE(tag.identifier_value, '') AS tag
FROM page p
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = p.tenant_id AND gi.goat_id = p.goat_id
    AND gi.status = 'active' AND gi.is_primary_for_goat
  LIMIT 1
) tag ON true
ORDER BY p.display_id, p.goat_id
`
