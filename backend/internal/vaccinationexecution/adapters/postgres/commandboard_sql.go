package postgres

// Command-board SQL, named and package-level.
//
// These fourteen statements used to be anonymous local string literals inside
// VaccinationCommandBoard. That placement is what let the endpoint's plans regress unnoticed:
// tools/scale-guard and the query-plan tests can only reach SQL they can NAME, so a hot-path
// statement declared as a function-local blob was structurally exempt from both. Hoisting them
// here makes every one of them addressable by
// commandboard_query_plan_test.go, and the inline-hot-path-SQL guard now rejects new local blobs
// in this package (tools/scale-guard).
//
// Nothing about the statements themselves changed in the move except where a comment says so.

// 1. KPIs query
// projection-review:
// (a) producer: obligation_id, status, due_at, batch_id | consumer: obligation_id GROUP BY none
// (b) completions pre-aggregated per obligation (1:1 after CTE), shed lookup 1:1
// (c) all KPI numerators count distinct animals, not obligation/dose fan-out.
//
//	doses_verified numerator: bool_or(status='accepted');
//	awaiting_verification numerator: bool_or(recorded unverified) AND NOT bool_or(accepted);
//	overdue_not_given numerator: OPEN AND (due_at's IST business date)<(asOf's IST business
//	date) AND no completions; scheduled_ahead numerator: OPEN AND no completions
//	AND (due_at's IST business date)>=(asOf's IST business date), denominator: all obligations.
//	Vaccination time grain is the IST business DAY, never an instant — a dose due today at
//	00:00 IST must never read overdue merely because as_of is later the same day.
//
// (d) OPEN is the repo's canonical open-obligation set
//
//	('scheduled','due','in_progress','deferred','missed') — the SAME set that defines
//	obligation_instances_open_logical_due_idx. It is NOT 'scheduled' alone: the sweeper flips
//	scheduled -> 'due' on the due business day, so a 'scheduled'-only predicate silently
//	dropped every currently-actionable obligation out of BOTH overdue_not_given and
//	scheduled_ahead (live CEO board read targets=40 but bucketed only 20 — the other park's
//	20 animals, with zero work done, were invisible to leadership).
//
// (e) BUCKET CONTRACT (docs/architecture/operational-read-model-contract.md,
//
//	"GET /vaccination/command — Grain and Buckets (disjoint unless noted)" + Bucket
//	Invariant): the FIVE numerator buckets are a DISJOINT and EXHAUSTIVE partition of
//	targets, evaluated as a priority chain —
//	  missed     = status 'missed', regardless of what else the animal holds
//	  verified   = has_accepted
//	  awaiting   = has_recorded_unverified AND NOT has_accepted
//	  overdue    = no completion AND OPEN AND due business date <  as_of business date
//	  scheduled  = no completion AND OPEN AND due business date >= as_of business date
//	  closed_without_dose = the residual: none of the above
//	so missed+verified+awaiting+overdue+scheduled+closed_without_dose = targets.
//
//	The chain used to be evaluated on the OBLIGATION row while targets counted DISTINCT
//	target_id — two different grains. An animal holding two obligations in different
//	states (one dose accepted, the next dose still scheduled) therefore satisfied two
//	bucket predicates on two different rows and was counted by BOTH COUNT(DISTINCT
//	target_id) expressions, while targets counted it once: the four tiles summed to more
//	than the total they sit under, and a CEO reading the board could not reconcile them.
//	That is the ordinary multi-dose case, not a corner case — every animal on a kid
//	schedule holds several doses at once.
//
//	The chain is now evaluated ONCE PER ANIMAL (per_animal below folds every one of that
//	animal's obligations into four booleans, then the priority chain picks exactly one),
//	so the buckets are disjoint at the SAME grain targets uses and the sum is restored.
//
//	PRECEDENCE: missed > verified > awaiting > overdue > scheduled, i.e. a missed dose wins
//	outright and otherwise the most-progressed dose wins. Below missed this is the same order
//	the per-obligation chain already used, so no tile changes meaning for a single-dose
//	animal; extending it unchanged to the animal grain keeps the contract one rule instead of
//	two. The consequence is stated rather than hidden: an animal with one accepted dose and
//	one overdue dose still reports as verified, so the tiles answer "how far has this animal
//	got" and NOT "how much work is outstanding" — the outstanding-work question is answered at
//	dose grain by the shed dose matrix and the verification queue below, which stay
//	per-obligation.
//
//	MISSED LEADS THE CHAIN, and it is the one exception to "most-progressed wins", because
//	letting verified lead made the board report the opposite of the truth. Folding to one row
//	per animal via bool_or means a single accepted dose anywhere in an animal's history sets
//	any_verified for good. On the live stg board 137 animals held a MISSED ET+TT dose; every
//	one of them also held an accepted dose of something else, so all 137 landed in
//	doses_verified and OVERDUE read 0 — a herd with 137 missed doses presented as fully green,
//	and the missed obligations were additionally invisible to any_overdue/any_scheduled
//	because each carried a recorded-but-unverified completion (no_completion = false). A
//	"how far has this animal got" reading cannot be allowed to answer "clear" for an animal
//	whose dose window closed unvaccinated: missed is not progress, it is the failure the board
//	exists to report, so it outranks every state an animal can simultaneously be in.
//
//	any_missed IS gated on no_completion -- bool_or(is_missed AND no_completion) -- and this
//	paragraph used to claim the opposite of the code sitting under it. The claim was wrong, not the
//	SQL: a missed obligation carrying a recorded-unverified completion is reported through
//	awaiting_verification, which is the bucket that actually describes it, while a missed dose with
//	NOTHING recorded is the failure missed_not_given exists to name.
//
//	The drilldown (commandBoardClosedWithoutDoseSQL) had copied this COMMENT rather than the code:
//	it folded any_missed ungated AND never applied it, so its drawer listed animals this tile does
//	not count. Both statements spell the fold identically now, and
//	commandboard_query_plan_test.go asserts the tile and the drawer agree ON DATA rather than
//	trusting either comment -- which is the only thing that would have caught this.
//
//	CLOSED WITHOUT DOSE is why the sum used to be <= targets rather than = targets. An
//	animal whose every obligation reached a closed status with no completion row against it
//	('canceled', 'waived', 'superseded', or a 'completed' whose completion was never
//	written) satisfies none of the four predicates: it is not open, so it cannot be overdue
//	or scheduled, and nothing was recorded, so it cannot be awaiting or verified. It still
//	counts in targets, because targets is COUNT(DISTINCT target_id) over the drive's
//	animals. A 100-animal drive with 3 withdrawn animals therefore read "Total 100" over
//	tiles summing to 97, and a leader could not tell whether that 3-animal hole was a
//	display bug, missing data, or three animals still owing work — the cheapest reading of
//	an unexplained gap is "the board is broken", which costs more trust than the three
//	animals are worth.
//
//	It is NAMED as a fifth bucket rather than subtracted out of targets. Subtracting would
//	also reconcile the arithmetic, but it would make targets drift below the roster the
//	operator was actually handed and below the cohort matrix's animal_count for the same
//	filter, and it would erase the withdrawal itself — which is the one fact in that hole a
//	leader can act on ("who took 3 animals off this drive, and why"). Naming it keeps the
//	total anchored to the roster and turns the silence into a number.
//
//	It is defined as the RESIDUAL of the other four rather than by enumerating closed
//	statuses, so the partition stays exhaustive by construction: adding a status to the
//	OPEN set above, or introducing a new terminal status, cannot reopen the gap.
//
// projection-review: membership=obligation_instances in the drive window, folded to one row per animal by per_animal; group_key=target_id (the ANIMAL), which is exactly the grain COUNT(DISTINCT target_id) uses for targets, so buckets and total share one key set; join_cardinality=comp is pre-aggregated per obligation before the fold, so a dose with several completions cannot multiply its animal, and every remaining join is 0..1 on a PK; pagination=NONE, these are whole-filter tile aggregates computed in the database and are page-size independent by construction; scope=tenant_id plus the capability-resolved park filter, parented through locations.parent_location_id
const commandBoardKPISQL = `
-- HERD MEMBERSHIP. Every read on this board joins goats and keeps only animals that are actually in
-- the herd, spelled as the same positive IN-list the rest of the backend uses
-- ('alive','sick','under_treatment','quarantine','icu').
--
-- It is a POSITIVE list on purpose. The filter here used to compare lifecycle_status against
-- 'terminated', which excluded NOTHING: that is not one of the values goats_lifecycle_status_check
-- permits, so the comparison is true for every row ever written. A dead, sold, culled, transferred
-- or lost animal sailed straight through a filter that looked like it was doing the job. A
-- negative list also silently readmits every status added later, which is how that hole would
-- reopen.
-- Merged animals are excluded separately via merged_into_goat_id, because a merge RETIRES the source
-- goat into another record and counting it is counting the same animal twice.
--
-- This matters because the board's own halves disagree otherwise: the cohort matrix reads the live herd while
-- these aggregates read obligation_instances directly, so a sold or merged goat still holding an
-- open obligation inflates targets and the shed cells while the cohort head count drops it -- two
-- numbers on one screen describing different herds, with no visible reconciliation break to hint at
-- it.
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
    COALESCE(comp.has_accepted, false) AS has_accepted,
    COALESCE(comp.has_recorded_unverified, false) AS has_recorded_unverified,
    comp.obligation_id IS NULL AS no_completion,
    oi.status = 'missed' AS is_missed,
    oi.status IN ('scheduled','due','in_progress','deferred','missed') AS is_open,
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
-- projection-review: membership=obligation_instances in the drive window, folded to ONE ROW PER ANIMAL here; group_key=target_id, the same grain COUNT(DISTINCT target_id) uses for targets, so the tiles and the total they sit under share one key set; join_cardinality=comp is pre-aggregated per obligation before this fold, so a dose with several completions cannot multiply its animal, and every other join is 0..1 on a PK; pagination=NONE, whole-filter tile aggregates computed in the database, page-size independent by construction; scope=tenant_id plus the capability-resolved park filter, parented through locations.parent_location_id
per_animal AS (
  SELECT
    target_id,
    -- MISSED means no dose reached the animal. An obligation swept to 'missed' that carries a
    -- recorded completion is a VERIFICATION backlog, not a missed dose: on stg all 137 such
    -- obligations were dosed on the day they were due. Counting them here reported 137 vaccinated
    -- animals as unvaccinated and simultaneously showed awaiting_verification = 0 while 137 proofs
    -- sat in the queue -- both tiles wrong, in opposite directions, from the same predicate.
    bool_or(is_missed AND no_completion) AS any_missed,
    bool_or(has_recorded_unverified AND NOT has_accepted) AS any_awaiting,
    bool_or(has_accepted) AS any_verified,
    bool_or(is_open AND no_completion AND due_before_as_of) AS any_overdue,
    bool_or(is_open AND no_completion AND NOT due_before_as_of) AS any_scheduled
  FROM scoped
  GROUP BY target_id
)
SELECT
  COUNT(*) AS targets,
  COUNT(*) FILTER (WHERE any_missed) AS missed_not_given,
  COUNT(*) FILTER (WHERE any_verified AND NOT any_awaiting AND NOT any_missed) AS doses_verified,
  COUNT(*) FILTER (WHERE any_awaiting AND NOT any_missed) AS awaiting_verification,
  COUNT(*) FILTER (WHERE any_overdue AND NOT any_awaiting AND NOT any_verified AND NOT any_missed) AS overdue_not_given,
  COUNT(*) FILTER (WHERE any_scheduled AND NOT any_overdue AND NOT any_awaiting AND NOT any_verified AND NOT any_missed) AS scheduled_ahead,
  COUNT(*) FILTER (WHERE NOT any_missed AND NOT any_verified AND NOT any_awaiting AND NOT any_overdue AND NOT any_scheduled) AS closed_without_dose
FROM per_animal
`

// 2. Cohort matrix query (management_stage × sex × vaccine × pending/submitted/verified)
//
// THREE disjoint buckets, partitioned by WHO OWES THE NEXT MOVE. pending_count used to fuse the
// first two, because obligation status advances only on VERIFICATION and never on submission:
// a park whose every animal had been vaccinated and submitted rendered byte-identically to a
// park nobody had touched, so the page showed "40 awaiting verification" in the KPI row and
// "40 pending" in the matrix directly below it with no column reconciling them, and a CEO could
// not tell that all 40 animals had in fact been vaccinated. The query was conformant to its
// written contract; the DEFINITION was the defect. Contract updated in the same change
// (docs/architecture/operational-read-model-contract.md, "Cohort Submission Matrix").
//
// projection-review:
// (a) PRODUCER unique column list: obligation_instances is unique on (obligation_id); the comp
//
//	CTE is pre-aggregated to exactly one row per obligation_id. Every bucket counts
//	DISTINCT oi.obligation_id, so one obligation contributes at most 1 to at most one bucket.
//	CONSUMER match/group column list: GROUP BY (park.location_id, park.name,
//	g.management_stage, g.sex, pr.dose_code) — the identical key set for all three buckets and
//	for animal_count; no bucket carries an extra or missing WHERE dimension.
//
// (b) Join multiplicity: comp is 1:1 on obligation_id (GROUP BY obligation_id in the CTE);
//
//	goats 1:1 on (target_id, tenant_id) (PK); protocol_rules 1:1 on (rule_id, tenant_id) (PK);
//	locations shed 1:1 on (scope_id, tenant_id) and park 1:1 on shed.parent_location_id. No
//	join fans the obligation grain out, so animal_count (DISTINCT goat_id) cannot multiply by
//	the number of vaccines or doses.
//
// (c) DISJOINT + TOTAL partition over the cell's obligations, evaluated per row from columns of
//
//	the SAME row (oi.status, oi.due_at, comp.has_accepted, comp.has_recorded_unverified), so
//	bucketing is a pure per-row partition with no cross-grain dependency:
//	  verified  = comp.has_accepted                                  (nobody owes anything)
//	  submitted = NOT has_accepted AND has_recorded_unverified       (the VERIFIER owes review)
//	  pending   = NOT has_accepted AND NOT has_recorded_unverified
//	              AND status IN (open set) AND due business date <= as-of business date
//	                                                                (the OPERATOR owes work)
//	has_accepted is checked first in every branch, so no obligation lands in two buckets, and
//	pending + submitted + verified <= the cell's obligation total.
//	Reconciliation key sets, shown identical: submitted_count's key set is
//	{obligation_id : has_recorded_unverified AND NOT has_accepted}, the obligation-grain image
//	of the KPI row's awaiting_verification key set {target_id : has_recorded_unverified AND
//	NOT has_accepted} computed over the SAME tenant/batch/park filter as the KPI query above;
//	the two agree exactly at one-obligation-per-animal-per-vaccine (the grain every live drive
//	uses), and the matrix stays obligation grain BY DESIGN so a multi-vaccine animal is
//	visible once per vaccine. Business-day grain, Asia/Kolkata, on both sides of the due-date
//	comparison — never an instant, never now()±N.
//	animal_count numerator: DISTINCT target_id per cohort; denominator: all non-terminated
//	animals in the cohort.
//
// (d) min/max_administered_at are MIN/MAX over the VERIFIED (accepted) completions of the same
//
//	obligation rows only — they are a date range read off the cell's verified bucket, never a
//	count, so they add no grain and cannot double count. They are NULL when verified_count is
//	0. They carry the real medical dates so a clubbed adult drive does not report its planned
//	date as the administration date.
const commandBoardCohortSQL = `
-- TWO NARROW HASH AGGREGATES OVER ONE SCAN SET, not one wide fold.
--
-- Three shapes were measured on the staging-scale tenant:
--
--   COUNT(DISTINCT g.goat_id) beside three COUNT(DISTINCT obligation_id) in one GROUP BY: a DISTINCT
--   aggregate forces SORTED aggregation over the whole input, which spilled to disk (external merge,
--   7.7 MB) -- 362ms of 441ms.
--
--   A per-(cell, animal) fold, then summing off it: removed the DISTINCTs, but the fold itself runs
--   at ~70k groups carrying two timestamps and three counters, so the planner still chose a sorted
--   GroupAggregate and STILL spilled (7.0 MB, 237ms of 265ms). Grouping on the park uuid instead of
--   its name shaved 5ms. The width was the problem, not the key.
--
--   This shape: the cell aggregates are computed DIRECTLY at cell grain, where there are a few
--   hundred groups, and the only figure that genuinely needs animal grain -- animal_count -- comes
--   from a DISTINCT over five narrow columns carrying no payload at all. Both hash in memory.
--
-- The equalities are exact, not approximate. pending/submitted/verified summed over a per-animal
-- fold equal COUNT(*) FILTER over the flat set, because every obligation belongs to exactly one
-- animal; MIN(MIN(x)) = MIN(x) and MAX(MAX(x)) = MAX(x); and COUNT(*) over one row per animal is
-- COUNT(DISTINCT goat_id). Verified against the live staging-scale tenant: this statement and the
-- original return byte-identical rows.
--
-- animal_count KEEPS its distinct grain because goat_id genuinely repeats -- that is the multi-dose
-- animal the matrix exists to show. The three bucket counts are obligation grain and must not be
-- deduplicated.
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
    bool_or(status = 'accepted') AS has_accepted,
    MIN(CASE WHEN status = 'accepted' THEN administered_at END) AS min_administered_at,
    MAX(CASE WHEN status = 'accepted' THEN administered_at END) AS max_administered_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
scoped AS (
  SELECT
    park.location_id AS park_id,
    g.management_stage,
    g.sex,
    pr.dose_code,
    g.goat_id,
    -- Obligation grain. COUNT(*) FILTER rather than COUNT(DISTINCT obligation_id): this set emits
    -- exactly one row per obligation (comp is pre-aggregated per obligation_id, and
    -- goats/protocol_rules/locations are all 1:1 on a primary key), so the DISTINCT could never
    -- remove a row and only forced a sort.
    (NOT COALESCE(comp.has_accepted, false)
      AND NOT COALESCE(comp.has_recorded_unverified, false)
      AND oi.status IN ('scheduled','due','in_progress','deferred','missed')
      AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
    ) AS is_pending,
    (NOT COALESCE(comp.has_accepted, false) AND COALESCE(comp.has_recorded_unverified, false)) AS is_submitted,
    COALESCE(comp.has_accepted, false) AS is_verified,
    CASE WHEN comp.has_accepted THEN comp.min_administered_at END AS min_administered_at,
    CASE WHEN comp.has_accepted THEN comp.max_administered_at END AS max_administered_at
  FROM obligation_instances oi
  JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
  JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
  LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
),
cell_totals AS (
  SELECT
    park_id, management_stage, sex, dose_code,
    COUNT(*) FILTER (WHERE is_pending)::bigint AS pending_count,
    COUNT(*) FILTER (WHERE is_submitted)::bigint AS submitted_count,
    COUNT(*) FILTER (WHERE is_verified)::bigint AS verified_count,
    MIN(min_administered_at) AS min_administered_at,
    MAX(max_administered_at) AS max_administered_at
  FROM scoped
  GROUP BY park_id, management_stage, sex, dose_code
),
cell_animals AS (
  SELECT park_id, management_stage, sex, dose_code, COUNT(*)::bigint AS animal_count
  FROM (
    SELECT DISTINCT park_id, management_stage, sex, dose_code, goat_id FROM scoped
  ) d
  GROUP BY park_id, management_stage, sex, dose_code
)
SELECT
  COALESCE(t.park_id::text, '') AS park_id,
  COALESCE(park.name, '') AS park_name,
  t.management_stage,
  t.sex,
  t.dose_code,
  a.animal_count,
  t.pending_count,
  t.submitted_count,
  t.verified_count,
  t.min_administered_at,
  t.max_administered_at
FROM cell_totals t
JOIN cell_animals a
  ON a.park_id IS NOT DISTINCT FROM t.park_id
 AND a.management_stage = t.management_stage
 AND a.sex = t.sex
 AND a.dose_code = t.dose_code
LEFT JOIN locations park ON park.location_id = t.park_id AND park.tenant_id = $1::uuid
ORDER BY park.name, t.management_stage, t.sex, t.dose_code
`

// 2a-bis. TRUE cohort head count, read from the live herd rather than from the obligations.
// The cohort query above can only see animals that carry an obligation for THAT dose, so the
// "Animals" column reported 229 for a cohort of 324 live adults — every animal whose Dose 1
// obligation had been closed out of the window vanished from its own head count. Head count is a
// herd fact, not an obligation fact, so it is read from goats.
// projection-review: membership=goats (live, shed-resolved); group_key=park x management_stage x sex; join_cardinality=locations 1:1 (shed -> park); pagination=bounded cohort aggregate; scope=tenant + optional park
// (a) producer unique columns: goat_id | consumer GROUP BY: park.location_id, management_stage, sex.
// (b) join multiplicity: shed 1:1 on goats.shed_id, park 1:1 on shed.parent_location_id — no fan-out.
// (c) key set: identical park x stage x sex key the cohort cells use, so the head count and the
//
//	cell counts describe the same cohort. Drive-batch scope is deliberately NOT applied: a
//	cohort's head count does not shrink because a drive covers part of it.
const commandBoardCohortHeadSQL = `
SELECT
  COALESCE(park.location_id::text, '') as park_id,
  g.management_stage,
  g.sex,
  COUNT(*) as head_count
FROM goats g
LEFT JOIN locations shed ON g.shed_id = shed.location_id AND g.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR park.location_id = $2::uuid)
GROUP BY park.location_id, g.management_stage, g.sex
`

// 3. Shed dose matrix query
// projection-review: membership=shed-scoped obligation_instances per tenant/batch/park; group_key=shed_id x dose_code x state; join_cardinality=comp CTE 1:1, protocol_rules 1:1, locations 1:1; pagination=bounded tenant aggregate (no paging); scope=tenant + optional batch + optional park EXISTS
// Detail (landed-review P1): grain=shed_id x dose_code x state — one row per cell, aggregated ONLY in the outer SELECT.
// (a) producer unique columns: obligation_id (with scope_id, dose_code, per-obligation state)
//
//	| consumer GROUP BY: shed_id, shed_name, dose_code, state
//
// (b) join multiplicity: completions pre-aggregated per obligation (comp CTE, 1:1),
//
//	protocol_rules 1:1 on rule_id, locations 1:1 on scope_id; inner CTE has NO GROUP BY
//	so due_at/state cannot split cells (landed-review P1 regression:
//	TestVaccinationCommandBoardShedDoseDateShiftOneCellPerState). overdue/scheduled state is an
//	IST business-DATE comparison ((due_at AT TIME ZONE 'Asia/Kolkata')::date vs
//	(asOf AT TIME ZONE 'Asia/Kolkata')::date), never an instant comparison — a dose due today at
//	00:00 IST stays 'scheduled' all day, never 'overdue' (TestVaccinationCommandBoardDueTodayDateShiftNotOverdue).
//
// (c) animal_count numerator: COUNT(DISTINCT target_id) over the cell's obligations;
//
//	denominator/key set: same shed x dose x state cell — identical key sets.
const commandBoardShedDoseSQL = `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
    MAX(CASE WHEN administered_at IS NOT NULL THEN administered_at END) as max_administered_at,
    MIN(CASE WHEN administered_at IS NOT NULL THEN administered_at END) as min_administered_at
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
shed_dose_obligations AS (
  -- per-OBLIGATION state; aggregation to the shed x dose x state cell happens ONLY in
  -- the outer SELECT so one cell is always exactly one row regardless of due dates.
  SELECT
    oi.scope_id as shed_id,
    loc.name as shed_name,
    CASE
      WHEN sp.shed_id IS NULL OR lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
      ELSE btrim(sp.partition_label)
    END AS partition_label,
    pr.dose_code,
    CASE
      WHEN comp.has_accepted THEN 'verified'
      WHEN comp.has_recorded_unverified THEN 'awaiting'
      WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date THEN 'overdue'
      WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') THEN 'scheduled'
      ELSE 'other'
    END as state,
    oi.target_id,
    comp.min_administered_at,
    comp.max_administered_at,
    CASE WHEN oi.status IN ('scheduled','due','in_progress','deferred','missed') THEN oi.due_at END as due_at
  FROM obligation_instances oi
  JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = oi.scope_id
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = oi.tenant_id
   AND sp.shed_id = oi.scope_id
   AND sp.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
   AND sp.status = 'active'
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.scope_type = 'shed'
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $4::uuid)
)
SELECT shed_id, shed_name, partition_label, dose_code, state,
  COUNT(DISTINCT target_id) as animal_count,
  MIN(min_administered_at) as min_administered_at,
  MAX(max_administered_at) as max_administered_at,
  MIN(due_at) as min_due_at,
  MAX(due_at) as max_due_at
FROM shed_dose_obligations
WHERE state != 'other'
GROUP BY shed_id, shed_name, partition_label, dose_code, state
ORDER BY shed_name, partition_label, dose_code, state
`

// 3b. Shed × VACCINE matrix, dose collapsed, reported as a flag.
//
// This is NOT a vaccine-collapsed rewrite of the dose matrix above, which stays dose-qualified
// because leadership asks per-dose figures and an earlier collapse was reverted for SUMMING
// Dose 1 + Dose 2 + Revaccination into a number that exceeded the cohort head count. The cell
// here carries no sum: state is bool_or over the shed's doses of that vaccine, so collapsing
// cannot over-count by construction. The two matrices answer different questions — "how much of
// each dose" and "is anything behind at all" — and neither can be derived from the other on the
// client without losing a guarantee.
//
// BEHIND is deliberately broader than the KPI row's missed bucket: an animal counts as behind
// when it holds a dose of this vaccine that is 'missed' OR is still open with its due business
// date already past, and has no accepted completion for it. A park head walking the shed cannot
// act on the distinction between "the sweeper has flipped this to missed" and "the sweeper has
// not run yet" — both mean the animal is unvaccinated past its window.
//
// projection-review: membership=obligation_instances in scope, scope_type='shed', joined to their rule's vaccine; group_key=(shed location_id, vaccine_code) — the cell grain the UI renders, so no client-side regrouping can change a cell's meaning; join_cardinality=comp pre-aggregated per obligation before the fold so a dose with several completions cannot multiply its animal, protocol_rule_dimensions is 1..N per rule_id (see (b)), locations 0..1 on PK; pagination=NONE, a bounded sheds × vaccines aggregate (11 × 6 on the live tenant) computed in the database; scope=tenant_id plus the capability-resolved park filter parented through locations.parent_location_id, PLUS the same scope_type='shed' guard the dose-matrix query carries, so the two cannot disagree about what counts as a shed.
// (a) producer unique columns: obligation_id | consumer GROUP BY: shed.location_id, d.vaccine_code.
// (b) join multiplicity: protocol_rule_dimensions is NOT 0..1 per rule_id — publish compiles one
//
//	rule into up to maxCompiledRuleDimensionsPerRule rows, one per selector combination. The
//	fan-out is harmless HERE, and only here, because vaccine_code is computed once per rule and
//	is therefore identical on every dimension row of that rule, while behind/total are
//	COUNT(DISTINCT target_id): the same (target_id, vaccine_code) pair collapses no matter how
//	many dimension rows the join emits. It is a real scan cost, not a real count error.
//
// (c) cap check: behind_animals <= total_animals by construction — the FILTER is a subset of the
//
//	same COUNT(DISTINCT target_id) key set.
//
// (d) scope_type='shed' is REQUIRED, not defensive. obligation_instances.scope_type also takes
//
//	'park', 'cohort', 'tenant' and 'custodian_party'. Without the guard a park-scoped
//	obligation joins locations on the PARK row and renders as a phantom shed named after the
//	park, and cohort/tenant/custodian-scoped rows miss the join entirely, COALESCE to an empty
//	shed id, and pile into a single blank row that silently mixes animals from everywhere.
const commandBoardShedVaccineSQL = `
-- THREE rewrites, all count-preserving, taking this statement from ~1.6s to the
-- hundreds-of-milliseconds class on the staging-scale tenant. It was the endpoint's slowest
-- remaining section once the drilldowns moved out, and therefore the whole board's critical path.
--
-- (1) rule_vaccine DE-FANS protocol_rule_dimensions BEFORE the join. That table is 1..N per
--     rule_id (publish compiles one rule into one row per selector combination) and carries the
--     SAME vaccine_code on every dimension row of a rule, so the join was multiplying all ~71k
--     obligation rows by ~3 before aggregating, purely to rediscover a value it already had. On the
--     live tenant 528 dimension rows reduce to 170 distinct (rule, vaccine) pairs.
--
--     NOTE, because an earlier version of this comment claimed otherwise: the rewrite does NOT
--     depend on vaccine_code being invariant per rule. DISTINCT (rule_id, tenant_id, vaccine_code)
--     preserves two codes as two rows, and vaccine_code is in per_animal's GROUP BY, so a rule that
--     ever compiled two codes still counts correctly -- exactly as the old COUNT(DISTINCT
--     target_id) did. The de-fan is a cost fix that happens to be safe for a stronger reason than
--     invariance. TestVaccinationCommandBoardShedVaccineOneToManyMultipleDimensions pins the count
--     under a deliberate 3-dimension fan-out.
--
-- (2) goat_partition resolves the partition label ONCE PER (goat, shed) instead of once per
--     obligation row. The label needs a regexp_replace to match shed_partitions.normalized_label;
--     evaluated inline it ran on every fanned obligation row (~210k times), and it now runs 1,679
--     times, once per goat_shed_partitions row. The CASE it produces is byte-identical.
--
-- (3) per_animal folds to one row per (cell, animal) and the outer query COUNTs those rows,
--     replacing THREE COUNT(DISTINCT target_id) passes over the same fanned set with one
--     GROUP BY. COUNT(DISTINCT x) FILTER (WHERE p) over a set equals COUNT(*) FILTER
--     (WHERE bool_or(p)) over that set grouped by x, so behind/verifying/total are unchanged --
--     including the invariant that behind_animals <= total_animals, which still holds by
--     construction because the FILTER ranges over a subset of the same rows.
--
-- BEHIND is deliberately broader than the KPI row's missed bucket: an animal counts as behind when
-- it holds a dose of this vaccine that is missed OR is still open with its due business date
-- already past, and has no accepted completion for it. A park head walking the shed cannot act on
-- the distinction between "the sweeper has flipped this to missed" and "the sweeper has not run
-- yet" -- both mean the animal is unvaccinated past its window.
--
-- scope_type='shed' is REQUIRED, not defensive. obligation_instances.scope_type also takes 'park',
-- 'cohort', 'tenant' and 'custodian_party'. Without the guard a park-scoped obligation joins
-- locations on the PARK row and renders as a phantom shed named after the park, and
-- cohort/tenant/custodian-scoped rows miss the join entirely, COALESCE to an empty shed id, and
-- pile into a single blank row that silently mixes animals from everywhere.
--
-- projection-review: membership=obligation_instances in scope, scope_type='shed', joined to their
-- rule's vaccine; group_key=(shed location_id, partition label, vaccine_code) -- the cell grain the
-- UI renders; join_cardinality=rule_vaccine is now 1:1 per rule_id (DISTINCT over an invariant
-- column), comp is pre-aggregated per obligation, goat_partition is 1:1 per (goat, shed) by its own
-- primary key, locations 0..1 on PK -- so nothing fans the obligation grain out and the per_animal
-- fold is a pure regrouping; pagination=NONE, a bounded sheds x vaccines aggregate computed in the
-- database; scope=tenant_id plus the capability-resolved park filter parented through
-- locations.parent_location_id.
WITH comp AS (
  SELECT obligation_id,
         bool_or(status = 'accepted') AS has_accepted,
         bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
rule_vaccine AS (
  SELECT DISTINCT rule_id, tenant_id, vaccine_code
  FROM protocol_rule_dimensions
  WHERE tenant_id = $1::uuid AND vaccine_code <> ''
),
goat_partition AS (
  SELECT
    gsp.goat_id,
    gsp.shed_id,
    CASE
      WHEN sp.shed_id IS NULL OR lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
      ELSE btrim(sp.partition_label)
    END AS partition_label
  FROM goat_shed_partitions gsp
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = gsp.tenant_id
   AND sp.shed_id = gsp.shed_id
   AND sp.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
   AND sp.status = 'active'
  WHERE gsp.tenant_id = $1::uuid
),
per_animal AS (
  SELECT
    oi.scope_id AS shed_id,
    COALESCE(gp.partition_label, '') AS partition_label,
    d.vaccine_code,
    oi.target_id,
    bool_or(
      NOT COALESCE(comp.has_accepted, false)
      AND NOT COALESCE(comp.has_recorded_unverified, false)
      AND (
        oi.status = 'missed'
        OR (oi.status IN ('scheduled','due','in_progress','deferred')
            AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
      )
    ) AS is_behind,
    bool_or(
      NOT COALESCE(comp.has_accepted, false)
      AND COALESCE(comp.has_recorded_unverified, false)
    ) AS is_verifying
  FROM obligation_instances oi
  JOIN rule_vaccine d ON d.rule_id = oi.rule_id AND d.tenant_id = oi.tenant_id
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN goat_partition gp ON gp.goat_id = g.goat_id AND gp.shed_id = oi.scope_id
  LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.scope_type = 'shed'
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
  GROUP BY 1, 2, 3, 4
)
SELECT
  shed.location_id::text AS shed_id,
  COALESCE(shed.name, '') AS shed_name,
  pa.partition_label,
  COALESCE(park.name, '') AS park_name,
  pa.vaccine_code,
  COUNT(*) FILTER (WHERE pa.is_behind)::bigint AS behind_animals,
  COUNT(*) FILTER (WHERE pa.is_verifying)::bigint AS verifying_animals,
  COUNT(*)::bigint AS total_animals
FROM per_animal pa
JOIN locations shed ON shed.location_id = pa.shed_id AND shed.tenant_id = $1::uuid
LEFT JOIN locations park ON park.location_id = shed.parent_location_id AND park.tenant_id = shed.tenant_id
GROUP BY shed.location_id, shed.name, pa.partition_label, park.name, pa.vaccine_code
`

// Columns come from the tenant's PUBLISHED vaccination protocol -- not from the cells above, and
// not from every dimension row that has ever existed.
//
// Deriving them from the CELLS would drop a vaccine the protocol requires but which generated no
// obligations anywhere, and that silence is exactly what a reader needs: an all-grey column then
// means "the protocol asks for this and nothing is planned", which is a real finding.
//
// Taking the WHOLE dimension table instead invents findings. Dimensions accumulate per protocol
// VERSION and retired versions keep their rows forever -- on this tenant BLUE_TONGUE exists only
// on a RETIRED version, so listing it produced an all-grey column that read as a protocol gap
// when the vaccine had simply been withdrawn. Published versions of the vaccination category are
// the source-of-truth boundary: what the herd is currently required to receive.
const commandBoardVaccineCodeSQL = `
SELECT DISTINCT d.vaccine_code
FROM protocol_rule_dimensions d
JOIN protocol_versions pv
  ON pv.protocol_version_id = d.protocol_version_id
 AND pv.tenant_id = d.tenant_id
WHERE d.tenant_id = $1::uuid
  AND d.vaccine_code <> ''
  AND d.category = 'vaccination'
  AND pv.status = 'published'
ORDER BY d.vaccine_code
`

// 4. Weekly given query (ISO week × vaccine × status)
// projection-review: membership=completions with administered_at; grain=ISO week × vaccine × status;
// join_cardinality=none
const commandBoardWeeklySQL = `
SELECT
  EXTRACT(ISOYEAR FROM vc.administered_at AT TIME ZONE 'Asia/Kolkata')::int as iso_year,
  EXTRACT(WEEK FROM vc.administered_at AT TIME ZONE 'Asia/Kolkata')::int as iso_week,
  pr.dose_code as dose_code,
  vc.status,
  COUNT(*) as count,
  MIN(vc.administered_at) as min_administered_at,
  MAX(vc.administered_at) as max_administered_at
FROM vaccination_completions vc
JOIN obligation_instances oi ON vc.obligation_id = oi.obligation_id AND vc.tenant_id = oi.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
WHERE vc.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND vc.administered_at IS NOT NULL
  AND vc.administered_at <= $2::timestamptz
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
    SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
  ))
GROUP BY iso_year, iso_week, pr.dose_code, vc.status
ORDER BY iso_year DESC, iso_week DESC, pr.dose_code, vc.status
`

// 5. Verification queue query (shed × dose awaiting verification)
// projection-review: rows = shed×dose with ≥1 recorded-unverified completion; grain=shed×dose;
// join_cardinality: shed 1:1, exists gate filters obligations with unverified completions only;
// row cardinality: one row per shed×dose pair that has ≥1 unverified completion (EXISTS ensures non-empty)
const commandBoardVerifyQueueSQL = `
SELECT
  oi.scope_id as shed_id,
  loc.name as shed_name,
  CASE
    WHEN sp.shed_id IS NULL OR lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
    ELSE btrim(sp.partition_label)
  END AS partition_label,
  pr.dose_code,
  COUNT(DISTINCT vc.completion_id) as awaiting_count,
  COUNT(DISTINCT oi.obligation_id) as total_count,
  MAX(vc.administered_at) as last_given_date,
  MIN(vc.administered_at) as first_given_date
FROM obligation_instances oi
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = oi.scope_id
LEFT JOIN shed_partitions sp
  ON sp.tenant_id = oi.tenant_id
 AND sp.shed_id = oi.scope_id
 AND sp.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
 AND sp.status = 'active'
LEFT JOIN vaccination_completions vc ON oi.obligation_id = vc.obligation_id AND vc.status = 'recorded' AND vc.verified_at IS NULL
LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
WHERE oi.tenant_id = $1::uuid
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND $2::timestamptz IS NOT NULL
  AND oi.scope_type = 'shed'
  AND EXISTS (
    SELECT 1 FROM vaccination_completions vc2
    WHERE vc2.obligation_id = oi.obligation_id AND vc2.status = 'recorded' AND vc2.verified_at IS NULL
  )
  AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
  AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $4::uuid)
GROUP BY oi.scope_id, loc.name, 3, pr.dose_code
ORDER BY shed_name, partition_label, pr.dose_code
`

// VaccinationCommandBoard returns the CEO closure view: KPIs, cohort matrix, shed dose matrix,
// weekly given, and verification queue. All reads are indexed canonical SQL (5k-50k envelope).
// driveOptionsSQL is the command board's drive picker catalogue, keyset-paginated.
//
// PAGE FIRST, DECORATE SECOND. This statement used to aggregate every (batch, park) group in the
// tenant -- distinct target/dose counts, a jsonb array of every shed and partition, and a per-day
// administration history -- and only then apply its LIMIT. On the staging-scale tenant that was
// 448ms and 753 KB for a dropdown, which made the PICKER the whole command board's critical path
// and put the response over the 512 KB budget on its own.
//
// It now resolves the ordered (batch, park) pairs from cheap columns, takes the page, and computes
// every expensive column against THAT PAGE ONLY. counts, shed_locs and the day history all join
// `page`, so their cost is proportional to the ~20 rows a caller asked for instead of the ~279
// groups in the tenant. Same rows out, same order, same JSON.
//
// KEYSET, NOT OFFSET, because OFFSET re-walks the skipped prefix on every page and is a banned
// scale anti-pattern in this repo. The sort is mixed-direction (status rank ascending, dates
// descending, then batch and park ascending), which a single row-comparison cannot express, so the
// resume predicate is written as the explicit lexicographic OR-chain below. sort_planned,
// sort_window and park_is_null exist so the ORDER BY and the keyset compare the SAME expressions --
// a keyset over NULLS FIRST/LAST that the ORDER BY spells differently silently skips the boundary
// row.
//
// The ordering key is TOTAL: batch_id breaks ties between two parks' rows of one batch, and
// (park_is_null, park_name) breaks ties within a batch. A keyset over a non-total order drops or
// repeats rows at every page boundary.
//
// projection-review: membership=obligation_instances in scope reduced to their distinct
// (batch, park) pairs; group_key=(batch_id, park_id), the row grain the picker renders;
// join_cardinality=protocol_rules INNER on the rule PK and both locations joins 1:1 so scoped does
// not fan out, and every decorating CTE joins `page` on its own group key; pagination=keyset,
// applied before decoration; scope=tenant_id plus the caller's park scope.
//
// scale-guard:ignore: 5k-50k-envelope — this statement has ELEVEN CTEs and the count is the fix,
// not the defect. god-cte exists to catch compute-on-read: reconstructing derived state from raw
// facts per request instead of reading a projection. Three of the CTEs here (pairs, ordered, page)
// were ADDED to stop doing that -- they resolve and page the picker's row set from cheap columns so
// counts, shed_locs and the day history decorate ~20 rows instead of the tenant's 279 groups. The
// previous eight-CTE version was the one that aggregated everything and limited afterwards, at
// 448ms and 753 KB. Bounded by commandboard_query_plan_test.go, which gates the executed plan
// rather than the CTE count, and by the /vaccination/command entry in
// tools/perf/hot-paths.vaccination.json. Per docs/decisions/operational-kernel-5k-50k-scale-envelope.md
// the command board is served from canonical indexed SQL at the current envelope; the anchor-date
// boundary on adding a projection instead is recorded in
// docs/runbooks/vaccination-command-board-latency.md.
// scale-guard:ignore: 5k-50k-envelope — see the paragraph above; the CTE count is the paging fix.
const driveOptionsSQL = `
-- PAIRS FIRST (lean), PAGE, THEN THE FAT SCAN RESTRICTED TO THE PAGE.
--
-- The previous rewrite paged before DECORATING but still built one wide scoped CTE over every
-- obligation in the tenant and re-scanned it three more times (pairs, counts, shed_locs): 70,843
-- rows materialised and walked four times, which is why the statement stayed at 448ms and remained
-- the board's slowest section. The header used to claim its cost was "proportional to the ~20 rows
-- a caller asked for"; that was false, and this is the shape that makes it true.
--
--   pairs   reads obligation_instances ONCE and keeps only what the picker's ROW SET needs:
--           (batch_id, park_id, park_name). No rule columns, no target_id, no shed columns.
--   page    orders and keysets those pairs, and takes the caller's page.
--   scoped  is the wide row set, built ONLY for the batches on that page -- so counts and
--           shed_locs read a page's obligations instead of the tenant's.
--
-- protocol_rules stays an INNER join in BOTH pairs and scoped: an obligation whose rule is missing
-- was never offered, and dropping it from pairs alone would let the picker list a drive whose
-- counts then came back empty.
--
-- KEYSET, NOT OFFSET, because OFFSET re-walks the skipped prefix on every page and is a banned
-- scale anti-pattern in this repo. The sort is mixed-direction (status rank ascending, dates
-- descending, then batch, park-null, park name and park id ascending), which a single row
-- comparison cannot express, so the resume predicate is the explicit lexicographic OR-chain below.
-- sort_planned, sort_window and park_is_null exist so the ORDER BY and the keyset compare the SAME
-- expressions -- a keyset over NULLS FIRST/LAST that the ORDER BY spells differently silently skips
-- the boundary row.
--
-- park_id IS THE FINAL TIE-BREAK, and it is what makes the order TOTAL. Park NAME is not unique:
-- nothing in the schema stops two parks under one tenant sharing a name (the live tenant has none
-- today, which is exactly why this would have gone unnoticed), and two same-named parks on one
-- batch would otherwise produce identical keys and drop a row at the page boundary.
--
-- projection-review: membership=obligation_instances in scope reduced to their distinct
-- (batch, park) pairs; group_key=(batch_id, park_id), the row grain the picker renders;
-- join_cardinality=protocol_rules INNER on the rule PK and both locations joins 1:1 so neither
-- pairs nor scoped fans out, and every decorating CTE joins page on its own group key;
-- pagination=keyset, applied before the wide scan and before decoration; scope=tenant_id plus the
-- caller's park scope.
--
-- scale-guard:ignore: 5k-50k-envelope -- this statement has eleven CTEs and the count is the fix,
-- not the defect: pairs/ordered/page exist to bound the work that follows them. Bounded by
-- commandboard_query_plan_test.go, which gates the executed plan rather than the CTE count, and by
-- the /vaccination/command entry in tools/perf/hot-paths.vaccination.json. The anchor-date boundary
-- on serving this from a projection instead is recorded in
-- docs/runbooks/vaccination-command-board-latency.md.
WITH pairs AS (
  -- The picker's ROW SET, from three columns. No counts, no arrays, no jsonb, no per-goat table.
  SELECT DISTINCT
    oi.batch_id,
    park.location_id AS park_id,
    park.name AS park_name
  FROM obligation_instances oi
  JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
  LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
  LEFT JOIN locations park ON park.location_id = loc.parent_location_id AND park.tenant_id = loc.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $2::uuid)
),
ordered AS (
  SELECT
    p.batch_id,
    p.park_id,
    p.park_name,
    b.status,
    b.planned_date,
    b.window_start,
    b.window_end,
    CASE b.status
      WHEN 'in_progress' THEN 0
      WHEN 'completed' THEN 1
      ELSE 2
    END AS status_rank,
    COALESCE(b.planned_date, '-infinity'::date) AS sort_planned,
    COALESCE(b.window_start, '-infinity'::timestamptz) AS sort_window,
    (p.park_name IS NULL) AS park_is_null,
    COALESCE(p.park_name, '') AS sort_park,
    COALESCE(p.park_id, '00000000-0000-0000-0000-000000000000'::uuid) AS sort_park_id
  FROM pairs p
  JOIN obligation_batches b ON b.batch_id = p.batch_id AND b.tenant_id = $1::uuid
),
page AS (
  SELECT *
  FROM ordered
  WHERE ($4::int IS NULL OR (
       status_rank > $4::int
    OR (status_rank = $4::int AND sort_planned < $5::date)
    OR (status_rank = $4::int AND sort_planned = $5::date AND sort_window < $6::timestamptz)
    OR (status_rank = $4::int AND sort_planned = $5::date AND sort_window = $6::timestamptz AND batch_id > $7::uuid)
    OR (status_rank = $4::int AND sort_planned = $5::date AND sort_window = $6::timestamptz AND batch_id = $7::uuid
        AND (park_is_null, sort_park, sort_park_id) > ($8::boolean, $9::text, $10::uuid))
  ))
  ORDER BY status_rank, sort_planned DESC, sort_window DESC, batch_id, park_is_null, sort_park, sort_park_id
  LIMIT $3
),
scoped AS (
  -- The WIDE row set, built only for the batches this page names. This is the scan that used to run
  -- over the whole tenant and then get re-walked three times.
  SELECT
    oi.batch_id,
    oi.obligation_id,
    oi.target_id,
    oi.scope_id,
    pr.dose_code,
    loc.location_id AS shed_id,
    loc.name        AS shed_name,
    loc.location_code AS shed_code,
    park.location_id AS park_id,
    park.name        AS park_name
  FROM obligation_instances oi
  JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
  LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
  LEFT JOIN locations park ON park.location_id = loc.parent_location_id AND park.tenant_id = loc.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $2::uuid)
    AND oi.batch_id IN (SELECT batch_id FROM page)
),
counts AS (
  SELECT
    s.batch_id,
    s.park_id,
    MAX(s.park_name) AS park_name,
    array_agg(DISTINCT s.dose_code) AS dose_codes,
    COUNT(DISTINCT s.target_id)::int AS target_count,
    -- COUNT(*), not COUNT(DISTINCT obligation_id): scoped emits exactly one row per obligation, so
    -- the DISTINCT could never remove a row and only bought a sort. target_count KEEPS its
    -- DISTINCT -- a goat legitimately repeats across its doses.
    COUNT(*)::int AS dose_count,
    COALESCE(array_agg(DISTINCT s.shed_name) FILTER (WHERE s.shed_name IS NOT NULL), ARRAY[]::text[]) AS shed_names
  FROM scoped s
  JOIN page pg ON pg.batch_id = s.batch_id AND pg.park_id IS NOT DISTINCT FROM s.park_id
  GROUP BY s.batch_id, s.park_id
),
shed_locs AS (
  -- The ONLY place the per-goat partition table is touched, and the jsonb object is built AFTER the
  -- DISTINCT rather than before it. Built before, jsonb_build_object ran once per obligation row and
  -- the DISTINCT then sorted the results down to a few hundred. Constructing the objects from
  -- de-duplicated scalar (shed, partition) pairs produces the identical array: same members, and
  -- jsonb_agg(... ORDER BY obj) still sorts by the object exactly as it did.
  SELECT batch_id, park_id, jsonb_agg(shed_obj ORDER BY shed_obj) AS shed_locations
  FROM (
    SELECT
      batch_id,
      park_id,
      jsonb_build_object(
        'shedId', shed_id::text,
        'shedName', COALESCE(NULLIF(shed_name, ''), shed_code, ''),
        'partition_label', partition_label
      ) AS shed_obj
    FROM (
      SELECT DISTINCT
        s.batch_id,
        s.park_id,
        s.shed_id,
        s.shed_name,
        s.shed_code,
        CASE
          WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
          ELSE btrim(gsp.partition_label)
        END AS partition_label
      FROM scoped s
      JOIN page pg ON pg.batch_id = s.batch_id AND pg.park_id IS NOT DISTINCT FROM s.park_id
      LEFT JOIN goat_shed_partitions gsp
        ON gsp.tenant_id = $1::uuid AND gsp.goat_id = s.target_id AND gsp.shed_id = s.scope_id
      WHERE s.shed_id IS NOT NULL
    ) pairs_inner
  ) objs
  GROUP BY batch_id, park_id
),
day_park AS (
  -- The former LATERAL, evaluated ONCE per (batch, park) instead of once per fanned row, and only
  -- for the batches on this page.
  SELECT
    vc.batch_id,
    day_loc.parent_location_id AS park_id,
    (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
    COUNT(DISTINCT vc.goat_id)::int AS target_count,
    COUNT(DISTINCT vc.obligation_id)::int AS dose_count
  FROM vaccination_completions vc
  JOIN obligation_instances day_oi ON day_oi.obligation_id = vc.obligation_id AND day_oi.tenant_id = vc.tenant_id
  LEFT JOIN locations day_loc ON day_oi.scope_id = day_loc.location_id AND day_oi.tenant_id = day_loc.tenant_id
  WHERE vc.tenant_id = $1::uuid
    AND EXISTS (SELECT 1 FROM page pg WHERE pg.batch_id = vc.batch_id)
  GROUP BY 1, 2, 3
),
day_park_json AS (
  SELECT batch_id, park_id,
         jsonb_agg(jsonb_build_object('date', to_char(day, 'YYYY-MM-DD'), 'targetCount', target_count, 'doseCount', dose_count) ORDER BY day) AS days
  FROM day_park
  WHERE park_id IS NOT NULL
  GROUP BY batch_id, park_id
),
day_any AS (
  -- A group whose shed resolves to no park took the LATERAL's park-is-null branch, which counted
  -- the batch's completions across EVERY park. Distinct-counting cannot be summed out of day_park,
  -- so that case is aggregated separately at its own grain.
  SELECT
    vc.batch_id,
    (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
    COUNT(DISTINCT vc.goat_id)::int AS target_count,
    COUNT(DISTINCT vc.obligation_id)::int AS dose_count
  FROM vaccination_completions vc
  WHERE vc.tenant_id = $1::uuid
    AND EXISTS (SELECT 1 FROM page pg WHERE pg.batch_id = vc.batch_id AND pg.park_id IS NULL)
  GROUP BY 1, 2
),
day_any_json AS (
  SELECT batch_id,
         jsonb_agg(jsonb_build_object('date', to_char(day, 'YYYY-MM-DD'), 'targetCount', target_count, 'doseCount', dose_count) ORDER BY day) AS days
  FROM day_any
  GROUP BY batch_id
)
SELECT
  pg.batch_id,
  COALESCE(pg.park_id::text, '') AS park_id,
  COALESCE(pg.park_name, '') AS park_name,
  pg.status,
  pg.planned_date,
  pg.window_start,
  pg.window_end,
  c.dose_codes,
  c.target_count,
  c.dose_count,
  COALESCE(CASE WHEN pg.park_id IS NULL THEN day_any_json.days ELSE day_park_json.days END, '[]'::jsonb) AS operator_days,
  c.shed_names,
  COALESCE(shed_locs.shed_locations, '[]'::jsonb) AS shed_locations,
  -- The keyset position of THIS row, so the caller can resume without re-deriving the sort keys.
  pg.status_rank,
  pg.sort_planned,
  pg.sort_window,
  pg.park_is_null,
  pg.sort_park,
  pg.sort_park_id
FROM page pg
JOIN counts c ON c.batch_id = pg.batch_id AND c.park_id IS NOT DISTINCT FROM pg.park_id
LEFT JOIN shed_locs ON shed_locs.batch_id = pg.batch_id AND shed_locs.park_id IS NOT DISTINCT FROM pg.park_id
LEFT JOIN day_park_json ON day_park_json.batch_id = pg.batch_id AND day_park_json.park_id = pg.park_id
LEFT JOIN day_any_json ON day_any_json.batch_id = pg.batch_id
ORDER BY pg.status_rank, pg.sort_planned DESC, pg.sort_window DESC, pg.batch_id, pg.park_is_null, pg.sort_park, pg.sort_park_id
`

// commandBoardCohortExceptionCTE is the dose-sequence exception set: animals holding an accepted
// LATER dose of the same vaccine course while THIS dose has no accepted completion.
//
// ONE definition, shared BY CONSTRUCTION between the board's COUNT
// (commandBoardCohortExceptionCountSQL) and the drilldown's LIST
// (commandBoardCohortExceptionListSQL). The tile and its drawer therefore cannot come to describe
// different animals through two copies drifting apart -- which is what the predicate-drift guard
// exists to stop, and what this shape makes structurally impossible rather than merely tested.
// $4..$7 are the OPTIONAL cell filter: the COUNT passes NULL for all four and gets the whole
// tenant, the LIST passes one cell.
//
// SET-BASED, NOT CORRELATED, AND THAT IS THE PERFORMANCE FIX. The predicate was previously two
// correlated EXISTS/NOT EXISTS probes re-evaluated per candidate row. Two shapes were measured on
// the staging-scale tenant and both failed:
//
//   - probes over a dose_family CTE: a CTE carries no statistics, so the planner estimated 29 rows
//     where 5,053 stood, chose a Nested Loop Anti Join, and discarded 13.9 MILLION rows -- 4s for
//     the COUNT and 18s for the cell-scoped LIST, past the 15s pool timeout. That timeout is the
//     reported 500.
//   - probes over the real protocol_rules table: real statistics fixed the LIST (283ms) but left
//     the tenant-wide COUNT at ~12s, because the family comparison is a regexp_replace on both
//     sides and nothing can index it per candidate.
//
// Computing each animal's accepted dose history ONCE and joining it is what works for both: the
// history is small (5,798 completion rows on the live tenant), max_accepted collapses "is there a
// later accepted dose of this family" to one comparison per (animal, family), and every join is an
// equijoin the planner hashes. 240ms tenant-wide.
//
// The joins are EXPLICIT rather than EXISTS/NOT EXISTS on purpose. Written as subquery probes
// against these same CTEs the planner still reached for a nested loop on its bad CTE row estimate;
// as an inner join plus an anti-join spelled LEFT JOIN ... IS NULL it hashes regardless of the
// estimate. max_accepted is one row per (animal, family), so the inner join cannot fan an animal
// out even when it holds three later accepted doses.
//
// Course family = the dose code with its position suffix removed (et_tt_adult_w1 -> et_tt_adult,
// fmd_kid_12w -> fmd_kid), so an adult Dose 2 never claims a kid-course Dose 1 is missing.
//
// Proven equal to the predicate it replaces: grouped to counts against the live staging-scale
// tenant, the old correlated statement and this one return byte-identical rows.
//
// projection-review: membership=obligation_instances of the same tenant/batch/park as the cohort
// matrix, joined to their rule; group_key=park x management_stage x sex x dose_code, the identical
// key set commandBoardCohortSQL groups by, so the count lands on the cell it describes;
// join_cardinality=goats 1:1 on target_id, protocol_rules 1:1 on rule_id, locations 0..1 on PK,
// max_accepted 1:1 per (animal, family) and accepted used only as an anti-join, so nothing fans the
// candidate grain out; pagination=the COUNT is a bounded per-cell aggregate, the LIST is keyset;
// scope=tenant + optional batch + optional park EXISTS, plus the optional cell.
const commandBoardCohortExceptionCTE = `
WITH accepted AS MATERIALIZED (
  -- The family regexp is computed INLINE, per obligation row, and an attempt to hoist it into a
  -- small per-rule CTE (204 rows instead of ~71k evaluations) made this statement THREE TIMES
  -- SLOWER -- 230ms to 725ms -- because a CTE carries no statistics and the planner mis-estimated
  -- the join off it. That is the same blindness that produced the 18-second cell-scoped list before
  -- this predicate was made set-based. Redundant CPU on a real table beats a cheap-looking CTE the
  -- planner cannot cost; do not "optimise" this back into a CTE without measuring it.
  SELECT DISTINCT
    oi.target_id,
    pr.dose_code,
    regexp_replace(pr.dose_code, '_(w[0-9]+|[0-9]+w|revac|booster|first)$', '') AS family,
    pr.sequence
  FROM obligation_instances oi
  JOIN vaccination_completions vc
    ON vc.obligation_id = oi.obligation_id AND vc.tenant_id = oi.tenant_id AND vc.status = 'accepted'
  JOIN protocol_rules pr ON pr.rule_id = oi.rule_id AND pr.tenant_id = oi.tenant_id
  WHERE oi.tenant_id = $1::uuid
),
max_accepted AS MATERIALIZED (
  SELECT target_id, family, max(sequence) AS max_sequence
  FROM accepted
  GROUP BY target_id, family
),
candidate AS MATERIALIZED (
  SELECT DISTINCT
    COALESCE(park.location_id::text, '') AS park_id,
    g.management_stage,
    g.sex,
    df.dose_code,
    regexp_replace(df.dose_code, '_(w[0-9]+|[0-9]+w|revac|booster|first)$', '') AS family,
    df.sequence,
    g.goat_id
    -- display_id and tenant_id are NOT carried here. They are only needed by the drilldown LIST,
    -- and this DISTINCT runs over every obligation in the tenant -- so carrying two extra columns
    -- widened seventy thousand rows to spare one join over a page of fifty. The list joins goats
    -- back by goat_id.
  FROM obligation_instances oi
  JOIN goats g ON oi.target_id = g.goat_id AND oi.tenant_id = g.tenant_id
  JOIN protocol_rules df ON oi.rule_id = df.rule_id AND oi.tenant_id = df.tenant_id
  LEFT JOIN locations shed ON oi.scope_id = shed.location_id AND oi.tenant_id = shed.tenant_id
  LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
  WHERE oi.tenant_id = $1::uuid
    AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $2::uuid)
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $3::uuid
    ))
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    -- THE OPTIONAL CELL. NULL on every one of these is the whole tenant, which is what the tile's
    -- COUNT asks for; the drawer passes all four.
    AND ($4::text IS NULL OR COALESCE(park.location_id::text, '') = $4::text)
    AND ($5::text IS NULL OR g.management_stage = $5::text)
    AND ($6::text IS NULL OR g.sex = $6::text)
    AND ($7::text[] IS NULL OR df.dose_code = ANY($7::text[]))
),
exceptions AS (
  -- THE OUTPUT GRAIN IS (cell, ANIMAL), and dropping sequence/family here is load-bearing.
  --
  -- candidate carries df.sequence because the later-dose join needs it, but a dose_code does NOT
  -- map to a single sequence: on the live tenant EIGHTEEN (tenant_id, dose_code) pairs carry more
  -- than one distinct sequence across rule versions, and 2,671 (animal, dose_code) pairs already
  -- hold obligations under rules whose sequences differ. Projecting c.* would then emit one row per
  -- (animal, sequence) and the tile would count an animal twice, while the drilldown's
  -- SELECT DISTINCT goat_id showed it once -- a tile reading 2 over a drawer listing 1, which is
  -- precisely the divergence this shared CTE exists to make impossible.
  --
  -- This is latent rather than live today only because the one dose code currently producing
  -- exceptions has a single sequence, which is why the old and new statements still returned
  -- byte-identical rows when diffed. Equal on today's data is not equal.
  SELECT DISTINCT
    c.park_id,
    c.management_stage,
    c.sex,
    c.dose_code,
    c.goat_id
  FROM candidate c
  -- THE LATER ACCEPTED DOSE. Inner join: no later dose, no exception. An animal whose dose_code
  -- resolves at two sequences qualifies if ANY of them has a later accepted dose, which is the same
  -- answer the old correlated EXISTS gave.
  JOIN max_accepted m
    ON m.target_id = c.goat_id AND m.family = c.family AND m.max_sequence > c.sequence
  -- THIS DOSE NOT ACCEPTED. Anti-join spelled as LEFT JOIN ... IS NULL so it hashes.
  LEFT JOIN accepted a
    ON a.target_id = c.goat_id AND a.dose_code = c.dose_code
  WHERE a.target_id IS NULL
)`

// commandBoardCohortExceptionCountSQL is the cohort matrix's dose-sequence exception COUNT.
//
// The COUNT stays on the board because it is a finding a reader must see without asking -- "321
// verified, 3 exceptions" is the whole point of the column. What left is the per-animal LIST, which
// now lives behind commandBoardCohortExceptionListSQL and is fetched per cell on demand.
//
// Splitting them is not merely a payload saving. The eager statement emitted one row per exception
// animal and decorated each with a correlated goat_identifiers lookup, so the board paid a
// per-animal scalar subquery for rows it then folded straight back into a counter. Counting in the
// database drops that work entirely while returning the identical number.
const commandBoardCohortExceptionCountSQL = commandBoardCohortExceptionCTE + `
SELECT park_id, management_stage, sex, dose_code, COUNT(*)::int AS exception_count
FROM exceptions
GROUP BY park_id, management_stage, sex, dose_code
`
