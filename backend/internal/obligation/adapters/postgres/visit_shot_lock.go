package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// visitShotLockNamespace prefixes every per-visit advisory-lock key so it cannot collide with
// other advisory-lock users in the database (e.g. CreateBatchWithObligations' own scope lock, or
// platform/worker's kernel-stage lock).
const visitShotLockNamespace = "goatos:obligation:visit-shot:"

// driveCapacityLockNamespace serializes the whole park/date vaccination drive capacity ledger.
// The unit is eligible animal slots for one tenant+park+planned_date, across all sheds, vaccines,
// operators, and planner paths.
const driveCapacityLockNamespace = "goatos:obligation:drive-capacity:"

// canonicalUUID parses id and re-renders it in Postgres's canonical lowercase-hex form (RV-03).
// hashtext() -- used by every advisory-lock key in this file -- hashes the RAW BYTES of its input
// text argument, so two textually different representations of the SAME Postgres uuid value (e.g.
// an uppercase-hex tenant id one caller passes vs the lowercase form another caller passes for the
// identical tenant) hash to two DIFFERENT lock ids unless every caller first normalizes to one
// canonical string form. Without this, two sweepers racing the same tenant/visit under
// differently-cased UUID strings would each acquire what they believe is "the" lock for that
// tenant/visit and run concurrently, defeating the single-writer guarantee LockTenantSweep and
// LockVisitShots exist to provide.
func canonicalUUID(id string) (string, error) {
	u, err := pgconv.UUID(id)
	if err != nil {
		return "", fmt.Errorf("invalid uuid %q: %w", id, err)
	}
	canon := pgconv.UUIDString(u)
	if canon == "" {
		return "", fmt.Errorf("empty or nil uuid %q", id)
	}
	return canon, nil
}

func (r *Repository) UpsertVaccinationDriveAssignments(ctx context.Context, tenantID string, assignments []domain.DriveAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("obligation: begin drive assignment tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := upsertVaccinationDriveAssignmentsTx(ctx, tx, tenant, assignments); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("obligation: commit drive assignments: %w", err)
	}
	committed = true
	return nil
}

// ReplaceVaccinationDriveAssignmentsForBatch persists the FINAL, attached-only drive-assignment
// row set for one (tenant, batch) atomically: delete every existing row for that batch, then
// insert exactly the supplied set, all inside one transaction. This is the F2 fix's persistence
// half -- an upsert alone can only add/update keys present in the new set, so a row for an
// obligation that was selected but did not actually attach (a shed/partition/operator key absent
// from the new set) would survive forever. Replace removes it. Idempotent/replay-safe: calling it
// twice with the identical assignment set leaves the same rows in place.
func (r *Repository) ReplaceVaccinationDriveAssignmentsForBatch(ctx context.Context, tenantID, batchID string, assignments []domain.DriveAssignment) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return fmt.Errorf("obligation: batch id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("obligation: begin drive assignment replace tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `
DELETE FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND batch_id = $2`, tenant, batch); err != nil {
		return fmt.Errorf("obligation: delete stale drive assignments: %w", err)
	}
	if len(assignments) > 0 {
		if err := upsertVaccinationDriveAssignmentsTx(ctx, tx, tenant, assignments); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("obligation: commit drive assignment replace: %w", err)
	}
	committed = true
	return nil
}

func upsertVaccinationDriveAssignmentsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, assignments []domain.DriveAssignment) error {
	batchIDs := make([]pgtype.UUID, 0, len(assignments))
	plannedDates := make([]time.Time, 0, len(assignments))
	operatorIDs := make([]pgtype.UUID, 0, len(assignments))
	parkIDs := make([]pgtype.UUID, 0, len(assignments))
	shedIDs := make([]pgtype.UUID, 0, len(assignments))
	physicalSheds := make([]string, 0, len(assignments))
	partitions := make([]string, 0, len(assignments))
	animalCounts := make([]int32, 0, len(assignments))
	vaccineRuleIDsJSON := make([]string, 0, len(assignments))
	totalDoses := make([]int32, 0, len(assignments))
	statuses := make([]string, 0, len(assignments))
	warningsJSON := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		batchID, err := pgconv.UUID(assignment.BatchID)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment batch id: %w", err)
		}
		parkID, err := pgconv.UUID(assignment.ParkID)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment park id: %w", err)
		}
		shedID := pgtype.UUID{}
		if assignment.ShedID != nil && strings.TrimSpace(*assignment.ShedID) != "" {
			shedID, err = pgconv.UUID(*assignment.ShedID)
			if err != nil {
				return fmt.Errorf("obligation: drive assignment shed id: %w", err)
			}
		}
		operatorID := pgtype.UUID{}
		if assignment.OperatorID != nil && strings.TrimSpace(*assignment.OperatorID) != "" {
			operatorID, err = pgconv.UUID(*assignment.OperatorID)
			if err != nil {
				return fmt.Errorf("obligation: drive assignment operator id: %w", err)
			}
		}
		ruleIDs := make([]string, 0, len(assignment.VaccineRuleIDs))
		for _, rawRuleID := range assignment.VaccineRuleIDs {
			rawRuleID = strings.TrimSpace(rawRuleID)
			if rawRuleID == "" {
				continue
			}
			if _, err := pgconv.UUID(rawRuleID); err != nil {
				return fmt.Errorf("obligation: drive assignment vaccine rule id: %w", err)
			}
			ruleIDs = append(ruleIDs, rawRuleID)
		}
		ruleIDsJSONBytes, err := json.Marshal(ruleIDs)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment vaccine rule ids: %w", err)
		}
		warnings := assignment.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		warningsJSONBytes, err := json.Marshal(warnings)
		if err != nil {
			return fmt.Errorf("obligation: drive assignment warnings: %w", err)
		}
		status := strings.TrimSpace(assignment.CapacityStatus)
		if status == "" {
			status = "within_cap"
		}
		physicalShed := strings.TrimSpace(assignment.PhysicalShed)
		if physicalShed == "" {
			physicalShed = "park"
		}
		partition := strings.TrimSpace(assignment.PartitionLabel)
		if partition == "" {
			partition = "whole"
		}
		batchIDs = append(batchIDs, batchID)
		plannedDates = append(plannedDates, biztime.BusinessDayStart(assignment.PlannedDate))
		operatorIDs = append(operatorIDs, operatorID)
		parkIDs = append(parkIDs, parkID)
		shedIDs = append(shedIDs, shedID)
		physicalSheds = append(physicalSheds, physicalShed)
		partitions = append(partitions, partition)
		animalCounts = append(animalCounts, assignment.AnimalCount)
		vaccineRuleIDsJSON = append(vaccineRuleIDsJSON, string(ruleIDsJSONBytes))
		totalDoses = append(totalDoses, assignment.TotalDoses)
		statuses = append(statuses, status)
		warningsJSON = append(warningsJSON, string(warningsJSONBytes))
	}
	if _, err := tx.Exec(ctx, `
	-- projection-review: membership=one generated DriveAssignment row per batch/date/operator/park/shed/physical_shed/partition; group_key=conflict key (tenant_id,batch_id,planned_date,park_id,shed_id,physical_shed,partition_label,operator_id); join_cardinality=no joins, UNNEST arrays are positional one-to-one inputs from the planner; pagination=single generated batch write, no paging or truncation; scope=explicit assignment park_id/shed_id.
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses, capacity_status, warnings
)
SELECT
  $1,
  u.batch_id,
  u.planned_date,
  u.operator_id,
  u.park_id,
  u.shed_id,
  u.physical_shed,
  u.partition_label,
  u.animal_count,
  COALESCE((
    SELECT array_agg(rule_id::uuid ORDER BY rule_id)
    FROM jsonb_array_elements_text(u.vaccine_rule_ids_json::jsonb) AS rule_ids(rule_id)
  ), '{}'::uuid[]),
  u.total_doses,
  u.capacity_status,
  u.warnings::jsonb
FROM unnest(
  $2::uuid[],
  $3::date[],
  $4::uuid[],
  $5::uuid[],
  $6::uuid[],
  $7::text[],
  $8::text[],
  $9::int[],
  $10::text[],
  $11::int[],
  $12::text[],
  $13::text[]
) AS u(batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids_json, total_doses, capacity_status, warnings)
	ON CONFLICT (
	  tenant_id,
	  batch_id,
	  planned_date,
	  park_id,
	  COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
	  physical_shed,
	  partition_label,
	  COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid)
	)
	DO UPDATE SET
  operator_id = EXCLUDED.operator_id,
  animal_count = EXCLUDED.animal_count,
  vaccine_rule_ids = EXCLUDED.vaccine_rule_ids,
  total_doses = EXCLUDED.total_doses,
  capacity_status = EXCLUDED.capacity_status,
  warnings = EXCLUDED.warnings,
  updated_at = now()`,
		tenant, batchIDs, plannedDates, operatorIDs, parkIDs, shedIDs, physicalSheds, partitions, animalCounts, vaccineRuleIDsJSON, totalDoses, statuses, warningsJSON); err != nil {
		return fmt.Errorf("obligation: upsert drive assignments: %w", err)
	}
	// Membership must be recorded in the SAME transaction as the assignment rows: a drive row that
	// stores only "80 animals" cannot be reduced by exactly one death/sale, and a split cell
	// ("200 on Jul 24, 124 on Jul 25") cannot say WHICH goat is on which day.
	if err := syncVaccinationDriveAssignmentMembersTx(ctx, tx, tenant, dedupUUIDs(batchIDs)); err != nil {
		return err
	}
	return nil
}

// zeroUUIDLiteral is the sentinel used by the assignment table's own partial-unique conflict key
// for a NULL shed_id; the membership cell key must group by the identical expression so a
// park-grain cell (shed_id IS NULL) forms exactly one cell instead of one cell per row.
const zeroUUIDLiteral = "'00000000-0000-0000-0000-000000000000'::uuid"

// syncVaccinationDriveAssignmentMembersTx recomputes the EXACT obligation/goat membership of every
// vaccination_drive_assignments row belonging to batchIDs, inside the caller's transaction.
//
// Why this exists (the closed gap): a drive assignment row stores an aggregate `animal_count`, not
// the identities behind it. When the planner splits one shed/partition cell across two operators or
// two dates, the database knew "200 here, 124 there" but not WHICH goats. Every downstream exact
// action -- remove one dead/sold goat from the one drive row that actually covers it, re-plan a cell
// after a cap change, show a per-animal operator/date, hand an operator an exact drive list -- needs
// per-goat truth.
//
// How membership is derived (deterministic, and the ONLY derivation available at this layer):
// domain.DriveAssignment carries no obligation ids -- the app-layer split
// (app.planVaccinationDriveAssignments) divides a cell purely by ANIMAL COUNT, so the caller does
// not know which goat landed on which arm either. Membership is therefore derived from canonical
// data with a stable rule:
//
//	cell key   = (batch_id, park_id, shed_id, physical_shed, partition_label, vaccine lane)
//	             where the vaccine lane is the row's own sorted vaccine_rule_ids. The lane is part of
//	             the key, not a filter applied once and forgotten: one shed/partition can hold two
//	             assignment rows planning DIFFERENT vaccines (two operator arms on the same day), and
//	             binding a goat's ET+TT obligation to the PPR row would make its exit/defer decrement
//	             the wrong operator's route. An empty lane (cardinality 0) is the legacy unspecific
//	             row and still admits the whole batch, as its own lane.
//	candidates = the batch's non-canceled goat obligations whose scope shed matches the cell
//	             (park-grain cells take the batch's non-shed-scoped obligations) and whose rule is
//	             in the cell's vaccine_rule_ids
//	goats      = candidates collapsed to distinct goat WITHIN a lane (the same unit animal_count counts)
//	allocation = goats ranked by goat_id are handed to the cell's split rows ordered by
//	             (planned_date, operator_id, assignment_id), each row taking exactly its own
//	             animal_count; every obligation of a goat follows that goat.
//
// Consequences stated plainly rather than papered over:
//   - count(DISTINCT goat_id) per assignment == that row's animal_count, EXCEPT when the declared
//     counts and the batch's real obligation set disagree. Membership is total: goats beyond the
//     declared total land on the cell's LAST split row rather than being orphaned, and a row whose
//     animal_count exceeds the remaining candidates simply gets fewer. Membership never invents a
//     goat to make a count agree.
//   - The member grain is one row per OBLIGATION (a goat due two vaccines in one drive has two
//     member rows), so the count that matches animal_count is DISTINCT goat_id, not row count.
//
// Idempotent/replay-safe: prune-then-insert scoped to these batches converges on the same rows for
// an unchanged plan. Set-based only (no per-obligation statement) -- see make scale-guard.
func syncVaccinationDriveAssignmentMembersTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, batchIDs []pgtype.UUID) error {
	if len(batchIDs) == 0 {
		return nil
	}
	// Prune first: an obligation moving from one split row to another would otherwise collide on
	// UNIQUE (tenant_id, obligation_id) before the stale row is gone.
	if _, err := tx.Exec(ctx, `
DELETE FROM vaccination_drive_assignment_members m
USING vaccination_drive_assignments vda
WHERE m.tenant_id = $1
  AND vda.tenant_id = $1
  AND vda.assignment_id = m.assignment_id
  AND vda.batch_id = ANY($2::uuid[])`, tenant, batchIDs); err != nil {
		return fmt.Errorf("obligation: prune drive assignment members: %w", err)
	}
	if _, err := tx.Exec(ctx, `
-- projection-review: membership=the batch's non-canceled goat obligation_instances, matched to the drive cell (batch,park,shed,physical_shed,partition,vaccine_lane) that actually covers them; group_key=cell key + vaccine lane + goat_id rank vs each split row's animal_count window; join_cardinality=obligation->cell is collapsed with DISTINCT ON (obligation_id) so a row can belong to exactly one assignment (also enforced by UNIQUE (tenant_id, obligation_id)), and goat->obligation fan-out is intentional (member grain is obligation, animal_count is matched by count(DISTINCT goat_id) within one lane); pagination=whole batch recomputed in one set-based statement, no page or LIMIT can truncate membership; scope=explicit batch id list within one tenant.
-- GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
--   producer unique key   = vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, COALESCE(shed_id,0), physical_shed, partition_label, COALESCE(operator_id,0)); its business lane is vaccine_rule_ids.
--   consumer match key    = (batch_id, park_id, COALESCE(shed_id,0), physical_shed, partition_label, lane_key) + goat rank window -- identical column list plus the lane the producer row declares; nothing the producer distinguishes is dropped.
--   row multiplicity      = split: 1 row per assignment. obl: exactly 1 row per obligation (DISTINCT ON) => obligation->assignment is N:1. goat_rank: 1 row per (cell,lane,goat) (SELECT DISTINCT) => alloc is 1:1 with (cell,lane,goat); final INSERT joins obl (N per goat) to alloc (1 per goat) = fan-out on the member grain only, never on the counters.
--   cap/ratio key sets    = the animal_count running window (lo/hi) and the grank it is compared against BOTH range over the same key set (batch, park, shed, physical_shed, partition_label, lane_key); an assignment's animal_count is therefore never sized against another vaccine's goats.
WITH a AS (
  SELECT assignment_id, batch_id, park_id, shed_id, physical_shed, partition_label,
         planned_date, operator_id, animal_count, vaccine_rule_ids,
         -- The vaccine lane is part of the cell's identity, not decoration: two rows can share one
         -- batch/park/shed/physical-shed/partition and plan DIFFERENT vaccines (different operator
         -- arms on the same day). Sorted so that two rows declaring the same rule SET in a different
         -- array order are one lane, not two lanes each claiming the whole cell.
         COALESCE((SELECT array_agg(r ORDER BY r) FROM unnest(vaccine_rule_ids) AS r), '{}'::uuid[]) AS lane_key
  FROM vaccination_drive_assignments
  WHERE tenant_id = $1 AND batch_id = ANY($2::uuid[])
), split AS (
  SELECT a.*,
    COALESCE(sum(a.animal_count) OVER w_ord, 0) - a.animal_count AS lo,
    COALESCE(sum(a.animal_count) OVER w_ord, 0) AS hi,
    row_number() OVER (
      PARTITION BY a.batch_id, a.park_id, COALESCE(a.shed_id, `+zeroUUIDLiteral+`), a.physical_shed, a.partition_label, a.lane_key
      ORDER BY a.planned_date DESC, a.operator_id DESC NULLS FIRST, a.assignment_id DESC) AS rn_last
  FROM a
  WINDOW w_ord AS (
    PARTITION BY a.batch_id, a.park_id, COALESCE(a.shed_id, `+zeroUUIDLiteral+`), a.physical_shed, a.partition_label, a.lane_key
    ORDER BY a.planned_date, a.operator_id NULLS LAST, a.assignment_id
    ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
), obl AS (
  SELECT DISTINCT ON (oi.obligation_id)
    oi.obligation_id, oi.target_id AS goat_id,
    s.batch_id, s.park_id, s.shed_id, s.physical_shed, s.partition_label, s.lane_key
  FROM obligation_instances oi
  JOIN split s
    ON s.batch_id = oi.batch_id
   AND ((oi.scope_type = 'shed' AND s.shed_id = oi.scope_id)
     OR (oi.scope_type <> 'shed' AND s.shed_id IS NULL))
   AND (cardinality(s.vaccine_rule_ids) = 0 OR oi.rule_id = ANY(s.vaccine_rule_ids))
  -- The animal's OWN physical placement. assignment_id is a random UUID, so ranking candidate
  -- cells by it made the goat -> cell binding nondeterministic run to run whenever one batch/shed
  -- held several cells that plan the same vaccine (different partition/operator/date). The goat's
  -- shed partition is the stable business key for "where this animal actually is"; only when the
  -- animal has no partition row do we fall back to the cell's own business keys.
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = oi.tenant_id
   AND gsp.goat_id = oi.target_id
  WHERE oi.tenant_id = $1
    AND oi.batch_id = ANY($2::uuid[])
    AND oi.target_type = 'goat'
    AND oi.status <> 'canceled'
  -- Lane tiebreak before assignment_id: when an obligation's rule is admitted by more than one lane
  -- (a specific lane and a legacy unspecific one), the specific lane wins deterministically instead
  -- of a random UUID picking the lane.
  ORDER BY oi.obligation_id,
           -- Partition-label canonicalization: the assignment row stores the DISPLAY form ("Part 1")
           -- while goat_shed_partitions stores the normalized form ("1"), so a raw equality never
           -- matched for numeric-partition sheds (Gandhi 1/2/3) and every goat fell through to the
           -- alphabetical s.partition_label tiebreak below -- collapsing all of a shed's partitions
           -- onto its first arm. Strip a leading "part " on both sides so the goat binds to its OWN
           -- partition cell. (BUG-029 follow-up: proven by the CPT reseed, Gandhi Part 3 goats were
           -- landing on Part 1 rows.)
           (gsp.partition_label IS NOT NULL
             AND regexp_replace(lower(btrim(s.partition_label)), '^part[[:space:]]+', '')
               = regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')) DESC,
           s.physical_shed, s.partition_label,
           (cardinality(s.lane_key) > 0) DESC, s.lane_key, s.assignment_id
), goat_rank AS (
  SELECT d.*, dense_rank() OVER (
    PARTITION BY d.batch_id, d.park_id, COALESCE(d.shed_id, `+zeroUUIDLiteral+`), d.physical_shed, d.partition_label, d.lane_key
    ORDER BY d.goat_id) AS grank
  FROM (
    SELECT DISTINCT batch_id, park_id, shed_id, physical_shed, partition_label, lane_key, goat_id FROM obl
  ) d
), alloc AS (
  SELECT g.batch_id, g.park_id, g.shed_id, g.physical_shed, g.partition_label, g.lane_key, g.goat_id, s.assignment_id
  FROM goat_rank g
  JOIN split s
    ON s.batch_id = g.batch_id
   AND s.park_id = g.park_id
   AND COALESCE(s.shed_id, `+zeroUUIDLiteral+`) = COALESCE(g.shed_id, `+zeroUUIDLiteral+`)
   AND s.physical_shed = g.physical_shed
   AND s.partition_label = g.partition_label
   AND s.lane_key = g.lane_key
   AND ((g.grank > s.lo AND g.grank <= s.hi) OR (s.rn_last = 1 AND g.grank > s.hi))
)
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT $1, al.assignment_id, o.obligation_id, o.goat_id
FROM obl o
JOIN alloc al
  ON al.batch_id = o.batch_id
 AND al.park_id = o.park_id
 AND COALESCE(al.shed_id, `+zeroUUIDLiteral+`) = COALESCE(o.shed_id, `+zeroUUIDLiteral+`)
 AND al.physical_shed = o.physical_shed
 AND al.partition_label = o.partition_label
 AND al.lane_key = o.lane_key
 AND al.goat_id = o.goat_id
ON CONFLICT (assignment_id, obligation_id) DO NOTHING`, tenant, batchIDs); err != nil {
		return fmt.Errorf("obligation: write drive assignment members: %w", err)
	}
	return nil
}

func dedupUUIDs(values []pgtype.UUID) []pgtype.UUID {
	seen := make(map[[16]byte]struct{}, len(values))
	out := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		if !value.Valid {
			continue
		}
		if _, ok := seen[value.Bytes]; ok {
			continue
		}
		seen[value.Bytes] = struct{}{}
		out = append(out, value)
	}
	return out
}

// visitShotLockKeyArg is the stable advisory-lock key argument for one (tenant, target, date)
// visit. tenantID and targetID are canonicalized (RV-03) before being folded into the key, so any
// two processes (two sweep passes, two concurrent worker replicas) compute the identical Postgres
// advisory-lock key for the same animal's visit and therefore mutually exclude regardless of
// process identity, call order, OR the textual case/representation of the UUIDs each caller holds.
func visitShotLockKeyArg(tenantID, targetID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock tenant id: %w", err)
	}
	targetCanon, err := canonicalUUID(targetID)
	if err != nil {
		return "", fmt.Errorf("obligation: visit-shot lock target id: %w", err)
	}
	return visitShotLockNamespace + tenantCanon + ":" + targetCanon + ":" + dateKey, nil
}

func driveCapacityLockKeyArg(tenantID, parkID, dateKey string) (string, error) {
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock tenant id: %w", err)
	}
	parkCanon, err := canonicalUUID(parkID)
	if err != nil {
		return "", fmt.Errorf("obligation: drive-capacity lock park id: %w", err)
	}
	return driveCapacityLockNamespace + tenantCanon + ":" + parkCanon + ":" + dateKey, nil
}

// pgxQuerier is satisfied by both *pgxpool.Pool and *pgxpool.Conn, letting countVisitShots run
// identically whether or not the caller already holds a dedicated locked connection.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// countVisitShots reads the persisted, already-committed shot count per target for one planned
// visit date: obligations attached (via batch_id) to a NON-canceled, non-superseded batch whose
// planned_date equals date. Bounded by the caller's explicit targetIDs (never a full-table scan),
// so it stays scale-safe at 1-5M animals regardless of how many total obligations/batches exist
// for the tenant.
func countVisitShots(ctx context.Context, q pgxQuerier, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	out := make(map[string]int32, len(targetIDs))
	if len(targetIDs) == 0 {
		return out, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	ids, err := obligationUUIDs(targetIDs)
	if err != nil {
		return nil, fmt.Errorf("obligation: visit shot target ids: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	// projection-review: membership=obligation_instances attached via oi.batch_id to obligation_batches (same tenant); group_key=oi.target_id for a single planned_date; join_cardinality=1:1 (each oi carries one batch_id and obligation_batches is keyed by (tenant_id, batch_id), so the JOIN is a semijoin to the batch's planned_date/status and COUNT(*) counts obligation rows, never a batch fan-out); pagination=bounded by the caller's explicit target_ids + one planned_date, computed whole (no user page — this is the cap-enforcement count, not a paged UI projection); scope=n/a (explicit target-id list, not a park/shed/cohort hierarchy)
	rows, err := q.Query(ctx, `
SELECT oi.target_id::text, count(*)::int
FROM obligation_instances oi
JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = $1
  AND oi.target_id = ANY($2::uuid[])
  AND ob.planned_date = $3::date
  AND ob.status NOT IN ('canceled', 'superseded')
  AND oi.status NOT IN ('canceled')
GROUP BY oi.target_id`, tenant, ids, pgconv.Date(&day))
	if err != nil {
		return nil, fmt.Errorf("obligation: count visit shots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetID string
		var count int32
		if err := rows.Scan(&targetID, &count); err != nil {
			return nil, fmt.Errorf("obligation: scan visit shot count: %w", err)
		}
		out[targetID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: visit shot count rows: %w", err)
	}
	return out, nil
}

// CountVisitShotsForTargets is the read-only half of the VAX-REV-01 fix (see
// app.visitShotCounter): the persisted shot count already committed for each of targetIDs on one
// planned visit date, from ANY prior sweep pass. Used to seed a fresh SweepSession so
// MaxShotsPerAnimalPerDrive is enforced across passes, not just within one. Safe to call without
// any lock -- callers that also need write-time atomicity against concurrent workers use
// LockVisitShots instead.
func (r *Repository) CountVisitShotsForTargets(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countVisitShots(ctx, r.pool, tenantID, targetIDs, date)
}

// LockVisitShots is the write-path half of the VAX-REV-01 fix (see app.visitShotLocker). It takes
// a session-level Postgres advisory lock (pg_advisory_lock, hashtext(visitShotLockKeyArg(...)))
// for every distinct target in targetIDs, in ascending sorted order (a stable order shared by
// every caller, so two workers racing over an overlapping target set always attempt their locks
// in the same order and cannot deadlock each other), on ONE dedicated connection acquired from the
// pool. It then reads the fresh persisted shot count for those now-locked targets and returns it
// alongside a release func.
//
// The lock is held on the dedicated connection until the caller invokes release, NOT scoped to
// any one transaction -- the caller's subsequent CreateBatchWithObligations call runs in its own,
// separate transaction/connection, and the advisory lock (a global, connection-independent
// Postgres primitive) is what actually serializes a concurrent caller attempting the very same
// (tenant, target, date) key, regardless of which connection eventually performs the write. A
// second caller's own LockVisitShots call for an overlapping key blocks on pg_advisory_lock until
// this caller's release() runs, so it always observes the first caller's committed write in its
// own fresh count read.
// projection-review: membership=explicit targetIDs for one tenant/date lock+count request; group_key=target_id in countVisitShots; join_cardinality=lock keys are deduplicated in memory and countVisitShots semijoins each obligation to one batch; pagination=bounded caller-provided target list, no page-local aggregate; scope=explicit target-id list.
func (r *Repository) LockVisitShots(ctx context.Context, tenantID string, targetIDs []string, date time.Time) (map[string]int32, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	keys := dedupNonBlank(targetIDs)
	if len(keys) == 0 {
		return map[string]int32{}, noop, nil
	}
	sort.Strings(keys)
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("obligation: acquire visit-shot lock connection: %w", err)
	}

	lockArgs := make([]string, 0, len(keys))
	for _, targetID := range keys {
		key, keyErr := visitShotLockKeyArg(tenantID, targetID, dateKey)
		if keyErr != nil {
			conn.Release()
			return nil, nil, fmt.Errorf("obligation: build visit-shot lock key: %w", keyErr)
		}
		lockArgs = append(lockArgs, key)
	}
	// Acquire every per-visit advisory lock in ONE round trip. unnest preserves array element order
	// and lockArgs is built from the already-sorted key set, so every caller acquires any shared
	// locks in the same global order and cannot deadlock. pg_advisory_lock is parallel-unsafe, so the
	// function scan runs sequentially in array order -- this is the batched, single-statement
	// equivalent of a per-target lock loop, without the per-target round trips.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext(k)) FROM unnest($1::text[]) AS t(k)", lockArgs); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return nil, nil, fmt.Errorf("obligation: lock visit shots: %w", err)
	}

	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}

	counts, err := countVisitShots(ctx, conn, tenantID, keys, date)
	if err != nil {
		_ = release(context.Background())
		return nil, nil, err
	}
	return counts, release, nil
}

func countDriveCellsForParkDate(ctx context.Context, q pgxQuerier, tenantID, parkID string, date time.Time) (int32, error) {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return 0, fmt.Errorf("obligation: drive capacity park id: %w", err)
	}
	day := biztime.BusinessDayStart(date)
	rows, err := q.Query(ctx, `
WITH drive_animals AS (
  -- projection-review: membership=scheduled obligation_instances attached to non-canceled batches on one planned_date, collapsed to DISTINCT target_id before counting; group_key=target_id for one tenant+park+date animal-cap ledger; join_cardinality=obligation_batches is keyed by (tenant_id,batch_id), goats/location are 1:1 lookup dimensions, and DISTINCT target_id prevents multi-vaccine obligation rows from consuming extra operator slots; pagination=full single park/date ledger count, no UI page or LIMIT can truncate capacity truth; scope=park scope accepts direct park batches, shed children, and goat park fallback under the explicit park_id.
  SELECT DISTINCT oi.target_id
  FROM obligation_instances oi
  JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN locations scope_loc
    ON scope_loc.tenant_id = ob.tenant_id
   AND scope_loc.location_id = ob.scope_id
  WHERE oi.tenant_id = $1
    AND ob.planned_date = $3::date
    AND ob.status NOT IN ('canceled', 'superseded')
    AND oi.status NOT IN ('canceled')
    AND (
      (ob.scope_type = 'park' AND ob.scope_id = $2)
      OR (ob.scope_type = 'shed' AND scope_loc.parent_location_id = $2)
      OR g.park_id = $2
    )
)
SELECT COALESCE(count(*), 0)::int FROM drive_animals`, tenant, park, pgconv.Date(&day))
	if err != nil {
		return 0, fmt.Errorf("obligation: count drive animals: %w", err)
	}
	defer rows.Close()
	var count int32
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("obligation: scan drive animal count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: drive animal count rows: %w", err)
	}
	return count, nil
}

// CountDriveCellsForParkDate returns persisted planned animal slots for one park/date drive, across all
// shed/park batch rows. The historical method name is kept for interface compatibility; stock/proof dose
// cells remain in planned_quantity and are not the operator-capacity unit.
func (r *Repository) CountDriveCellsForParkDate(ctx context.Context, tenantID, parkID string, date time.Time) (int32, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return countDriveCellsForParkDate(ctx, r.pool, tenantID, parkID, date)
}

// LockDriveCapacity serializes capacity planning for one tenant/park/date. The caller must hold
// the returned release until its batch attach/update is committed or abandoned.
func (r *Repository) LockDriveCapacity(ctx context.Context, tenantID, parkID string, date time.Time) (int32, func(context.Context) error, error) {
	dateKey := biztime.BusinessDayStart(date).Format("2006-01-02")
	lockArg, err := driveCapacityLockKeyArg(tenantID, parkID, dateKey)
	if err != nil {
		return 0, nil, err
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("obligation: acquire drive-capacity lock connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockArg); err != nil {
		_ = releaseVisitShotConn(context.Background(), conn)
		return 0, nil, fmt.Errorf("obligation: lock drive capacity: %w", err)
	}
	release := func(releaseCtx context.Context) error {
		return releaseVisitShotConn(releaseCtx, conn)
	}
	count, err := countDriveCellsForParkDate(ctx, conn, tenantID, parkID, date)
	if err != nil {
		_ = release(context.Background())
		return 0, nil, err
	}
	return count, release, nil
}

// PickVaccinationOperatorForDrive returns the currently least-loaded active vaccination operator for
// the park/date. The planner uses the returned member as the durable batch conducted_by assignment;
// when every available operator is already at cap, the least-loaded operator is still returned so
// last-safe work is recorded as over-cap/overtime instead of being silently pushed later.
func (r *Repository) PickVaccinationOperatorForDrive(ctx context.Context, tenantID, parkID string, date time.Time, capPerOperator int32) (*string, error) {
	operators, err := r.AvailableVaccinationOperatorsForDrive(ctx, tenantID, parkID, date, capPerOperator)
	if err != nil {
		return nil, err
	}
	if len(operators) == 0 {
		return nil, nil
	}
	operatorID := operators[0].OperatorID
	return &operatorID, nil
}

func (r *Repository) AvailableVaccinationOperatorsForDrive(ctx context.Context, tenantID, parkID string, date time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error) {
	return r.availableVaccinationOperatorsForDrive(ctx, tenantID, parkID, "", date, capPerOperator)
}

// AvailableVaccinationOperatorsForDriveExcludingBatch is the BUG-041 rebuild-path read: it computes
// each operator's REMAINING capacity for the park/date exactly like AvailableVaccinationOperatorsForDrive
// but excludes excludeBatchID's own obligations from the persisted load, so a batch being rebuilt does
// not count against its own operators (item 4: capacity self-counting). Other batches on the same
// operator/date still count in full.
func (r *Repository) AvailableVaccinationOperatorsForDriveExcludingBatch(ctx context.Context, tenantID, parkID, excludeBatchID string, date time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error) {
	return r.availableVaccinationOperatorsForDrive(ctx, tenantID, parkID, excludeBatchID, date, capPerOperator)
}

func (r *Repository) availableVaccinationOperatorsForDrive(ctx context.Context, tenantID, parkID, excludeBatchID string, date time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return nil, fmt.Errorf("obligation: park id: %w", err)
	}
	excludeBatch := pgtype.UUID{}
	if strings.TrimSpace(excludeBatchID) != "" {
		excludeBatch, err = pgconv.UUID(excludeBatchID)
		if err != nil {
			return nil, fmt.Errorf("obligation: exclude batch id: %w", err)
		}
	}
	rows, err := r.pool.Query(ctx, `
WITH capacity_config AS (
  SELECT COALESCE((SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id = $1), NULLIF($4::int, 0), 200)::int AS default_cap
),
candidate AS (
  SELECT wm.workforce_member_id, wm.updated_at,
         COALESCE(MAX(wp.vaccination_daily_animal_cap), (SELECT default_cap FROM capacity_config))::int AS daily_cap
  FROM workforce_members wm
  JOIN locations park_loc
    ON park_loc.tenant_id = $1
   AND park_loc.location_id = $2
   AND park_loc.status = 'active'
  JOIN workforce_positions wp
    ON wp.tenant_id = wm.tenant_id
   AND wp.workforce_member_id = wm.workforce_member_id
   AND wp.status = 'active'
   AND wp.valid_from <= $3::date + interval '1 day'
   AND (wp.valid_to IS NULL OR wp.valid_to > $3::date)
   AND wp.position_tier <> 'director'
   AND (
     (wp.scope_type = 'center' AND wp.scope_id IN ($2, park_loc.parent_location_id))
     OR (wp.scope_type = 'shed' AND wp.scope_id IN (
       SELECT location_id FROM locations WHERE tenant_id = $1 AND parent_location_id = $2 AND location_type = 'shed' AND status = 'active'
     ))
   )
  JOIN position_module_duties pmd
    ON pmd.tenant_id = wp.tenant_id
   AND pmd.position_code = wp.position_code
   AND pmd.status = 'active'
   AND pmd.effective_from <= $3::date + interval '1 day'
   AND (pmd.effective_to IS NULL OR pmd.effective_to > $3::date)
   AND pmd.duty_type = 'execute'
   AND pmd.module_code IN ('preventive_care', 'vaccination', 'pc.vaccination')
  WHERE wm.tenant_id = $1
    AND wm.status = 'active'
    AND NOT EXISTS (
      SELECT 1
      FROM workforce_absences wa
      WHERE wa.tenant_id = wm.tenant_id
        AND wa.workforce_member_id = wm.workforce_member_id
        AND wa.status IN ('approved', 'escalation_required')
        AND wa.starts_at < $3::date + interval '1 day'
        AND wa.ends_at > $3::date
    )
    AND NOT EXISTS (
      SELECT 1
      FROM workforce_positions offpos
      WHERE offpos.tenant_id = wm.tenant_id
        AND offpos.workforce_member_id = wm.workforce_member_id
        AND offpos.status = 'active'
        AND offpos.valid_from <= $3::date + interval '1 day'
        AND (offpos.valid_to IS NULL OR offpos.valid_to > $3::date)
        AND offpos.week_off_weekday = lower(to_char($3::date, 'FMDay'))
    )
  GROUP BY wm.workforce_member_id, wm.updated_at
), assignment_load AS (
  -- projection-review: membership=exact vaccination_drive_assignment_members rows for drive-assignment cells on one park/planned_date/operator; group_key=vda.operator_id for one tenant+park_id+planned_date after filtering exact member obligations by status; join_cardinality=vda:members is 1:N at exact member grain and members:obligation_instances is N:1 by obligation_id, collapsed with COUNT(DISTINCT m.goat_id) so multi-vaccine obligations do not inflate operator animal load; pagination=full available-operator candidate set for one park/date, no keyset paging; scope=explicit vda.park_id with execution date vda.planned_date and status matrix ob.status plus oi.status.
  SELECT vda.operator_id AS workforce_member_id, count(DISTINCT m.goat_id)::int AS animals
  FROM vaccination_drive_assignments vda
  JOIN obligation_batches ob
    ON ob.tenant_id = vda.tenant_id
   AND ob.batch_id = vda.batch_id
  JOIN vaccination_drive_assignment_members m
    ON m.tenant_id = vda.tenant_id
   AND m.assignment_id = vda.assignment_id
  JOIN obligation_instances oi
    ON oi.tenant_id = m.tenant_id
   AND oi.obligation_id = m.obligation_id
  WHERE vda.tenant_id = $1
    AND vda.park_id = $2
    AND vda.planned_date = $3::date
    AND vda.operator_id IS NOT NULL
    AND ($5::uuid IS NULL OR vda.batch_id <> $5)
    AND ob.status IN ('planned', 'in_progress')
    AND oi.status IN ('scheduled', 'due', 'in_progress')
  GROUP BY vda.operator_id
), legacy_batch_load AS (
  -- Legacy fallback for planned batches that predate exact drive-assignment rows. Once a batch has
  -- any vaccination_drive_assignments, assignment_load above is authoritative for that batch so
  -- split operators/dates/partitions cannot be collapsed back to obligation_batches.conducted_by.
  SELECT ob.conducted_by AS workforce_member_id, count(DISTINCT oi.target_id)::int AS animals
  FROM obligation_batches ob
  JOIN obligation_instances oi
    ON oi.tenant_id = ob.tenant_id
   AND oi.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1
    AND ob.scope_type = 'park'
    AND ob.scope_id = $2
    AND ob.planned_date = $3::date
    AND ob.status IN ('planned', 'in_progress')
    AND ob.conducted_by IS NOT NULL
    AND ($5::uuid IS NULL OR ob.batch_id <> $5)
    AND oi.status IN ('scheduled', 'due', 'in_progress')
    AND NOT EXISTS (
      SELECT 1
      FROM vaccination_drive_assignments vda
      WHERE vda.tenant_id = ob.tenant_id
        AND vda.batch_id = ob.batch_id
    )
  GROUP BY ob.conducted_by
), load AS (
  -- projection-review: membership=assignment_load plus legacy_batch_load already reduced to one row per operator for the same tenant+park+planned_date; group_key=workforce_member_id; join_cardinality=UNION ALL of two pre-aggregated operator-load sources followed by GROUP BY workforce_member_id, so no member or obligation rows are joined at this layer; pagination=full available-operator candidate set for one park/date, no keyset paging; scope=explicit park/date/status filters inherited from both source CTEs.
  SELECT workforce_member_id, sum(animals)::int AS animals
  FROM (
    SELECT workforce_member_id, animals FROM assignment_load
    UNION ALL
    SELECT workforce_member_id, animals FROM legacy_batch_load
  ) all_load
  GROUP BY workforce_member_id
)
SELECT c.workforce_member_id::text, GREATEST(c.daily_cap - COALESCE(l.animals, 0), 0)::int
FROM candidate c
LEFT JOIN load l ON l.workforce_member_id = c.workforce_member_id
ORDER BY
  CASE WHEN c.daily_cap <= 0 THEN 0 WHEN COALESCE(l.animals, 0) < c.daily_cap THEN 0 ELSE 1 END,
  COALESCE(l.animals, 0) ASC,
  c.updated_at ASC NULLS FIRST,
  c.workforce_member_id ASC`, tenant, park, biztime.BusinessDayStart(date), capPerOperator, excludeBatch)
	if err != nil {
		return nil, fmt.Errorf("obligation: list vaccination operators: %w", err)
	}
	defer rows.Close()
	out := make([]domain.DriveOperatorCapacity, 0)
	for rows.Next() {
		var operator domain.DriveOperatorCapacity
		if err := rows.Scan(&operator.OperatorID, &operator.Cap); err != nil {
			return nil, fmt.Errorf("obligation: scan vaccination operator: %w", err)
		}
		out = append(out, operator)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: vaccination operator rows: %w", err)
	}
	filtered, err := r.applyVaccinationOperatorAssignmentConfig(ctx, tenant, park, biztime.BusinessDayStart(date), out)
	if err != nil {
		return nil, err
	}
	return filtered, nil
}

func (r *Repository) applyVaccinationOperatorAssignmentConfig(ctx context.Context, tenant, park pgtype.UUID, date time.Time, operators []domain.DriveOperatorCapacity) ([]domain.DriveOperatorCapacity, error) {
	if len(operators) == 0 {
		return operators, nil
	}
	var cfg vaccexecd.OperatorAssignmentConfig
	err := r.pool.QueryRow(ctx, `
SELECT park_id::text, active_operators_per_day, default_operator_id::text, row_version
FROM vaccination_operator_assignment_config
WHERE tenant_id = $1 AND park_id = $2`, tenant, park).Scan(&cfg.ParkID, &cfg.ActiveOperatorsPerDay, &cfg.DefaultOperatorID, &cfg.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return operators, nil
	}
	if err != nil {
		return nil, fmt.Errorf("obligation: operator assignment config: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
SELECT operator_id::text, shift_label, shift_start_minute, shift_end_minute, COALESCE(week_off_weekday, '')
FROM vaccination_operator_shift_config
WHERE tenant_id = $1 AND park_id = $2`, tenant, park)
	if err != nil {
		return nil, fmt.Errorf("obligation: operator shift config: %w", err)
	}
	defer rows.Close()
	shifts := make([]vaccexecd.OperatorShift, 0)
	for rows.Next() {
		var shift vaccexecd.OperatorShift
		if err := rows.Scan(&shift.OperatorID, &shift.ShiftLabel, &shift.ShiftStartMinute, &shift.ShiftEndMinute, &shift.WeekOffWeekday); err != nil {
			return nil, fmt.Errorf("obligation: operator shift config scan: %w", err)
		}
		shift.ParkID = cfg.ParkID
		shifts = append(shifts, shift)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: operator shift config rows: %w", err)
	}
	if len(shifts) == 0 {
		// Config row exists but no shift rows: the resolver can't determine the
		// default/cover operator, so we cannot honor the config. Fail closed rather
		// than silently returning the unfiltered operator list (fail-open).
		return nil, domain.ErrOperatorAssignmentConfigPresentButEmpty
	}

	leaveRows, err := r.pool.Query(ctx, `
SELECT workforce_member_id::text, starts_at, ends_at
FROM workforce_absences
WHERE tenant_id = $1
  AND status IN ('approved', 'escalation_required')
  AND starts_at < $2::date + interval '1 day'
  AND ends_at > $2::date`, tenant, date)
	if err != nil {
		return nil, fmt.Errorf("obligation: operator assignment leaves: %w", err)
	}
	defer leaveRows.Close()
	leaves := make([]vaccexecd.OperatorLeaveWindow, 0)
	for leaveRows.Next() {
		var leave vaccexecd.OperatorLeaveWindow
		if err := leaveRows.Scan(&leave.OperatorID, &leave.Start, &leave.End); err != nil {
			return nil, fmt.Errorf("obligation: operator assignment leaves scan: %w", err)
		}
		leaves = append(leaves, leave)
	}
	if err := leaveRows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: operator assignment leaves rows: %w", err)
	}

	resolution, err := vaccexecd.ResolveOperatorsForDriveDay(date, cfg, shifts, leaves)
	if err != nil {
		return nil, fmt.Errorf("obligation: resolve operator assignment config: %w", err)
	}
	byID := make(map[string]domain.DriveOperatorCapacity, len(operators))
	for _, operator := range operators {
		byID[strings.TrimSpace(operator.OperatorID)] = operator
	}
	filtered := make([]domain.DriveOperatorCapacity, 0, len(resolution.AvailableOperators))
	for _, operatorID := range resolution.AvailableOperators {
		if operator, ok := byID[strings.TrimSpace(operatorID)]; ok {
			filtered = append(filtered, operator)
		}
	}
	if len(filtered) == 0 {
		// Config present but the resolved operator(s) are not in the executable
		// candidate set (all off/leave with no cover, or resolved-but-not-executable):
		// fail closed so the sweeper defers this day instead of planning at base cap
		// with a possibly-unassigned drive.
		return nil, domain.ErrOperatorAssignmentConfigPresentButEmpty
	}
	return filtered, nil
}

// tenantSweepLockNamespace prefixes the single per-tenant whole-sweep advisory-lock key so it
// cannot collide with the per-visit locks or any other advisory-lock user.
const tenantSweepLockNamespace = "goatos:obligation:tenant-sweep:"

// LockTenantSweep takes ONE tenant-scoped session advisory lock that serializes the ENTIRE
// obligation sweep (preflight + every version) for a tenant across processes (RV-03). It uses
// pg_try_advisory_lock (non-blocking): if another sweeper already holds it -- a second kernel-worker
// replica, or the standalone cmd/obligation-sweeper running alongside the kernel stage -- acquired
// is false and the caller SKIPS this run, since the in-progress writer already covers the tenant.
// When acquired, this sweep is the single priority-ordered writer for the tenant, so its in-memory
// priority arbitration (SortSweepVersionsByPriority + SweepSession.visitClaims) is authoritative and
// no concurrent lower-priority writer can commit a competing shot mid-sweep and invert the medical
// plan by lock-acquisition order. The caller MUST call the returned release exactly once.
func (r *Repository) LockTenantSweep(ctx context.Context, tenantID string) (bool, func(context.Context) error, error) {
	// RV-03: canonicalize tenantID BEFORE folding it into the lock key. Without this, an uppercase-
	// and a lowercase-hex string for the SAME tenant uuid hash to two different advisory-lock ids
	// (hashtext hashes raw text bytes, not the parsed uuid value), so two sweepers for the same
	// tenant could both observe pg_try_advisory_lock == true and run concurrently -- exactly the
	// single-writer race this lock exists to prevent.
	tenantCanon, err := canonicalUUID(tenantID)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: tenant-sweep lock tenant id: %w", err)
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("obligation: acquire tenant-sweep lock connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", tenantSweepLockNamespace+tenantCanon).Scan(&acquired); err != nil {
		conn.Release()
		return false, nil, fmt.Errorf("obligation: try tenant-sweep lock: %w", err)
	}
	if !acquired {
		// No advisory lock is held on this connection, so a plain Release (no unlock) is correct.
		conn.Release()
		return false, func(context.Context) error { return nil }, nil
	}
	return true, func(releaseCtx context.Context) error { return releaseVisitShotConn(releaseCtx, conn) }, nil
}

// visitShotUnlockTimeout bounds the independent cleanup context releaseVisitShotConn uses so a
// canceled caller context can never skip pg_advisory_unlock_all and leak session locks (RV-05).
const visitShotUnlockTimeout = 5 * time.Second

// releaseVisitShotConn releases every session advisory lock this dedicated connection holds in ONE
// round trip (pg_advisory_unlock_all frees all locks the session owns) and returns the connection
// to the pool. Using unlock_all rather than a per-key unlock loop keeps release a single call and
// cannot leak a lock on the pooled connection regardless of how large the key set was.
//
// RV-05: the unlock runs on an independent, bounded context derived with context.WithoutCancel, so
// a caller whose context is already canceled (a shut-down sweep, a timed-out request) still frees
// its session locks instead of silently skipping the unlock. If the unlock nonetheless fails, the
// connection may still own session advisory locks; returning it to the pool would leave an
// invisible lock that blocks every later worker on that visit key forever, so it is DESTROYED
// (hijacked out of the pool and closed) rather than released back.
func releaseVisitShotConn(ctx context.Context, conn *pgxpool.Conn) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), visitShotUnlockTimeout)
	defer cancel()
	if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock_all()"); err != nil {
		hijacked := conn.Hijack()
		_ = hijacked.Close(cleanupCtx)
		return fmt.Errorf("obligation: unlock visit shots (connection destroyed to avoid leaking session locks): %w", err)
	}
	conn.Release()
	return nil
}

func dedupNonBlank(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
