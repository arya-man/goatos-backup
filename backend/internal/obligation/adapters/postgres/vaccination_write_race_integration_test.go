package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Two-connection races between every vaccination write path and a concurrent anchor change.
//
// Connection B opens a transaction that moves the clinical floor LATER (a DOB correction, an
// accepted-intake/arrival correction, or a newly accepted first-wave completion) and holds it
// uncommitted. Connection A then runs one public repository write that is valid against the OLD
// anchors but not against the new ones. B commits, A finishes.
//
// The committed state is then read back through a fresh connection. Every row that A inserted,
// moved, reopened or rebound must satisfy the floor proved against the COMMITTED anchors; A must
// not finish while B is uncommitted (it has to wait on the anchor lock), and A may only fail with
// a clinical rejection or a serialization failure.
//
// Only public repository methods are exercised, so the tests survive the internal carry-over and
// drive-override refactors.

type raceChange struct {
	name string
	// ruleDose is the dose whose floor the change moves.
	ruleDose string
	// goat seeding: dob/arrival/purpose, plus first-wave history for the completion case.
	dob, arrival *time.Time
	purpose      string
	history      []string
	// original is valid under both old and new anchors' predecessor state; target is valid under
	// the old anchors only; newFloor is the floor under the committed (new) anchors.
	original, target, newFloor time.Time
	vaccineCode                string
}

func raceRules() []contractRule {
	return []contractRule{
		{dose: "et_tt_kid", code: "ET_TT", name: "ET+TT", trigger: "birth_age", offset: 28, sequence: 1},
		{dose: "ppr_arrival", code: "PPR", name: "PPR", trigger: "post_arrival", offset: 14, sequence: 1},
		{dose: "goat_pox_w2", code: "GOAT_POX", name: "Goat Pox", trigger: "manual_campaign", sequence: 1},
		{dose: "et_tt_w1", code: "ET_TT", name: "ET+TT", trigger: "manual_campaign", sequence: 2},
		{dose: "ppr_w1", code: "PPR", name: "PPR", trigger: "manual_campaign", sequence: 2},
	}
}

func raceChanges() []raceChange {
	dob := contractDay(time.September, 1)
	arrival := contractDay(time.September, 1)
	return []raceChange{
		// DOB 09-01 -> 09-11: birth_age+28 floor 09-29 -> 10-09.
		{name: "dob", ruleDose: "et_tt_kid", dob: &dob, vaccineCode: "ET_TT",
			original: contractDay(time.September, 29), target: contractDay(time.October, 1), newFloor: contractDay(time.October, 9)},
		// Accepted intake 09-01 -> 09-11: post_arrival+14 floor 09-15 -> 09-25.
		{name: "arrival", ruleDose: "ppr_arrival", arrival: &arrival, purpose: "breeding", vaccineCode: "PPR",
			original: contractDay(time.September, 15), target: contractDay(time.September, 22), newFloor: contractDay(time.September, 25)},
		// A concurrently ACCEPTED first-wave completion is not ordered against these writes: the
		// completion path does not lock the goat row. The next generation pass re-proves the row.
		// Ordering it is follow-up scope (see the progress doc).
	}
}

// raceFixture is one (path, change) scenario: its own protocol, version and goat.
type raceFixture struct {
	change    raceChange
	protoID   string
	versionID string
	rules     map[string]string
	goat      string
}

var raceGoatSeq int

func newRaceFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, path string, c raceChange) raceFixture {
	t.Helper()
	raceGoatSeq++
	goat := fmt.Sprintf("10000000-0000-4000-8000-0000000e%04x", raceGoatSeq)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.race."+path+"."+c.name)
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1, fatteningPlanDSL, raceRules(), true)
	seedContractGoat(t, ctx, pool, goat, "goat", c.dob, c.purpose, c.arrival)
	for _, dose := range c.history {
		seedAcceptedAdministration(t, ctx, pool, v, rules[dose], goat, contractDay(time.September, 1))
	}
	return raceFixture{change: c, protoID: protoID, versionID: v, rules: rules, goat: goat}
}

// beginAnchorChange opens connection B's uncommitted anchor change.
func (f raceFixture) beginAnchorChange(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin anchor change: %v", err)
	}
	later := contractDay(time.September, 11)
	switch f.change.name {
	case "dob":
		_, err = tx.Exec(ctx, `UPDATE goats SET dob = $2 WHERE goat_id = $1::uuid`, f.goat, later)
	case "arrival":
		_, err = tx.Exec(ctx, `UPDATE procurement_load_goats SET warmup_started_at = $2, intake_accepted_at = $2 WHERE goat_id = $1::uuid`, f.goat, later)
	case "completion":
		// A newly accepted, verified first-wave dose: the acceptance trigger (migration 000401)
		// takes the goat row lock that every vaccination write proves under.
		at := contractDay(time.September, 20)
		var holder string
		if err = tx.QueryRow(ctx, `
INSERT INTO obligation_instances (tenant_id, protocol_version_id, rule_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'goat', $4::uuid, 'park', $5::uuid, $6, 'completed', 'race-late-first-wave-' || $4::text, 2)
RETURNING obligation_id::text`, tenantID, f.versionID, f.rules["et_tt_w1"], f.goat, cbePark, at).Scan(&holder); err == nil {
			_, err = tx.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'accepted', $4, 'race-late-first-wave-' || $3::text)`, tenantID, holder, f.goat, at)
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("anchor change %s: %v", f.change.name, err)
	}
	return tx
}

type raceRowState struct {
	due     time.Time
	version string
	status  string
}

// snapshotGoatRows reads every obligation of the goat under the fixture's rule dose (any version).
func (f raceFixture) snapshotGoatRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]raceRowState {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT oi.obligation_id::text, oi.due_at, oi.protocol_version_id::text, oi.status
FROM obligation_instances oi
JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
WHERE oi.tenant_id = $1::uuid AND oi.target_id = $2::uuid AND pr.dose_code = $3`, tenantID, f.goat, f.change.ruleDose)
	if err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	defer rows.Close()
	out := map[string]raceRowState{}
	for rows.Next() {
		var id string
		var s raceRowState
		if err := rows.Scan(&id, &s.due, &s.version, &s.status); err != nil {
			t.Fatalf("scan snapshot: %v", err)
		}
		out[id] = s
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	return out
}

func isOpenRaceStatus(status string) bool {
	switch status {
	case "scheduled", "due", "in_progress":
		return true
	}
	return false
}

// assertNoWrittenRowBelowFloor compares the committed state against the pre-race snapshot: every
// row A created, moved, reopened or rebound (and that is open) must sit on/after the new floor.
func (f raceFixture) assertNoWrittenRowBelowFloor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, before map[string]raceRowState) {
	t.Helper()
	after := f.snapshotGoatRows(t, ctx, pool)
	for id, now := range after {
		prev, existed := before[id]
		written := !existed || !prev.due.Equal(now.due) || prev.version != now.version || (prev.status != now.status && isOpenRaceStatus(now.status))
		if !written || !isOpenRaceStatus(now.status) {
			continue
		}
		if now.due.Before(f.change.newFloor) {
			t.Fatalf("committed %s row %s (version %s, status %s) at %s is below the committed %s floor %s",
				f.change.ruleDose, id, now.version, now.status, now.due.Format("2006-01-02"), f.change.name, f.change.newFloor.Format("2006-01-02"))
		}
	}
}

// race runs write on connection A while B's anchor change is uncommitted, then commits B.
func (f raceFixture) race(t *testing.T, ctx context.Context, pool *pgxpool.Pool, write func() error) error {
	t.Helper()
	tx := f.beginAnchorChange(t, ctx, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	done := make(chan error, 1)
	go func() { done <- write() }()
	select {
	case err := <-done:
		t.Fatalf("write finished (err=%v) while the %s change was uncommitted; it must wait on the anchor lock", err, f.change.name)
	case <-time.After(700 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit %s change: %v", f.change.name, err)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatalf("write never finished after the %s change committed", f.change.name)
		return nil
	}
}

func requireClinicalOrSerializationRejection(t *testing.T, path string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) || errors.Is(err, ports.ErrVaccinationNotApplicable) || isSerializationFailure(err) {
		return
	}
	t.Fatalf("%s after concurrent anchor change: unexpected error %v", path, err)
}

func TestVaccinationInsertSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	for _, c := range raceChanges() {
		t.Run(c.name, func(t *testing.T) {
			f := newRaceFixture(t, ctx, pool, "insert", c)
			before := f.snapshotGoatRows(t, ctx, pool)
			err := f.race(t, ctx, pool, func() error {
				_, _, err := repo.InsertObligation(ctx, contractRow(f.versionID, f.rules[c.ruleDose], f.goat, "race-insert-"+f.goat, c.target))
				return err
			})
			requireClinicalOrSerializationRejection(t, "insert", err)
			f.assertNoWrittenRowBelowFloor(t, ctx, pool, before)
		})
	}
}

func TestVaccinationRescheduleSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	for _, c := range raceChanges() {
		t.Run(c.name, func(t *testing.T) {
			f := newRaceFixture(t, ctx, pool, "reschedule", c)
			id := seedLegacyRow(t, ctx, pool, f.versionID, f.rules[c.ruleDose], f.goat, "race-resched-row-"+f.goat, "", c.newFloor.AddDate(0, 1, 0))
			before := f.snapshotGoatRows(t, ctx, pool)
			err := f.race(t, ctx, pool, func() error {
				_, _, err := repo.RescheduleObligationByID(ctx, tenantID, id, "race-resched-"+f.goat, []string{cbePark},
					c.target, c.target, nil, time.Now().UTC())
				return err
			})
			requireClinicalOrSerializationRejection(t, "reschedule", err)
			f.assertNoWrittenRowBelowFloor(t, ctx, pool, before)
		})
	}
}

func TestVaccinationDeferredReopenSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	for _, c := range raceChanges() {
		t.Run(c.name, func(t *testing.T) {
			f := newRaceFixture(t, ctx, pool, "reopen", c)
			key := "race-deferred-" + f.goat
			in := contractRow(f.versionID, f.rules[c.ruleDose], f.goat, key, c.target)
			in.Status = "deferred"
			// Valid against the old anchors: the held row is written before the race.
			if _, applied, err := repo.InsertDeferredObligation(ctx, in, "sick", contractDay(time.September, 12)); err != nil || !applied {
				t.Fatalf("seed deferred row: applied=%v err=%v", applied, err)
			}
			before := f.snapshotGoatRows(t, ctx, pool)
			err := f.race(t, ctx, pool, func() error {
				_, _, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, key, time.Now().UTC(), nil)
				return err
			})
			requireClinicalOrSerializationRejection(t, "deferred reopen", err)
			f.assertNoWrittenRowBelowFloor(t, ctx, pool, before)
		})
	}
}

func TestVaccinationDriveDateOverrideSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	// The override does many round trips before and inside its tx; over the OCI link 5s is not
	// enough, and a timeout would mask the race outcome.
	repo := NewRepository(pool, 60*time.Second)

	const operator = "20000000-0000-4000-8000-0000000e0c01"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'RACE-A', 'Race Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'race_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'race_operator_a', 'manager', 'sunday', 50, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operator, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	for _, c := range raceChanges() {
		t.Run(c.name, func(t *testing.T) {
			f := newRaceFixture(t, ctx, pool, "override", c)
			if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code, vaccine_type, pathogen_class, min_gap_days, vaccine_json)
VALUES ($1, $2, $3, 'vaccination', 'race-' || $4, $4, $5, 'killed', 'bacterial', 0, '{"course_type":"single"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO NOTHING`,
				tenantID, f.versionID, f.rules[c.ruleDose], c.ruleDose, c.vaccineCode); err != nil {
				t.Fatalf("seed rule dimension: %v", err)
			}
			id := seedLegacyRow(t, ctx, pool, f.versionID, f.rules[c.ruleDose], f.goat, "race-override-row-"+f.goat, "", c.original)
			original := c.original
			batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
				TenantID: tenantID, ProtocolVersionID: f.versionID, ScopeType: "park", ScopeID: cbePark,
				Session: "race-override:" + f.goat, PlannedDate: &original, Status: "planned",
				EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
			}, []string{id})
			if err != nil || attached != 1 || batchID == "" {
				t.Fatalf("attach override row to batch: batch=%q attached=%d err=%v", batchID, attached, err)
			}
			if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
				BatchID: batchID, PlannedDate: original, OperatorID: testStringPtr(operator), ParkID: cbePark,
				PhysicalShed: "Race Shed", PartitionLabel: "1", AnimalCount: 1,
				VaccineRuleIDs: []string{f.rules[c.ruleDose]}, TotalDoses: 1, CapacityStatus: "within_cap",
			}}); err != nil {
				t.Fatalf("seed drive assignment: %v", err)
			}
			before := f.snapshotGoatRows(t, ctx, pool)
			err = f.race(t, ctx, pool, func() error {
				_, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
					TenantID: tenantID, ParkID: cbePark, VaccineCode: c.vaccineCode, OriginalDriveDate: original,
					OverrideDate: c.target, Reason: "race " + c.name, CreatedBy: operator,
				})
				return err
			})
			requireClinicalOrSerializationRejection(t, "drive override", err)
			f.assertNoWrittenRowBelowFloor(t, ctx, pool, before)
		})
	}
}

func TestVaccinationCarryOverSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	for _, c := range raceChanges() {
		t.Run(c.name, func(t *testing.T) {
			f := newRaceFixture(t, ctx, pool, "carryover", c)
			// Race victim: valid against the old anchors, below the new floor.
			seedLegacyRow(t, ctx, pool, f.versionID, f.rules[c.ruleDose], f.goat, "race-carry-row-"+f.goat, "", c.target)

			// Identity witness on an unaffected goat: an old animal with a far, valid row that sits in a
			// batch, a drive assignment and a member row. Carry-over must rebind it in place.
			raceGoatSeq++
			witness := fmt.Sprintf("10000000-0000-4000-8000-0000000e%04x", raceGoatSeq)
			oldDOB := contractDay(time.January, 1).AddDate(-2, 0, 0)
			seedContractGoat(t, ctx, pool, witness, "goat", &oldDOB, "", nil)
			witnessDue := contractDay(time.December, 1)
			witnessKey := "race-carry-witness-" + witness
			witnessID := seedLegacyRow(t, ctx, pool, f.versionID, f.rules["et_tt_kid"], witness, witnessKey, "", witnessDue)
			batchID := seedDriveMembershipBatch(t, ctx, pool, f.versionID, cbePark, witnessDue)
			if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET batch_id=$2::uuid WHERE obligation_id=$1::uuid`, witnessID, batchID); err != nil {
				t.Fatalf("attach witness batch: %v", err)
			}
			var assignmentID string
			if err := pool.QueryRow(ctx, `
INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses)
VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, NULL, 'Race 1', 'whole', 1, ARRAY[$5::uuid], 1)
RETURNING assignment_id::text`, tenantID, batchID, witnessDue, cbePark, f.rules["et_tt_kid"]).Scan(&assignmentID); err != nil {
				t.Fatalf("seed witness assignment: %v", err)
			}
			if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`, tenantID, assignmentID, witnessID, witness); err != nil {
				t.Fatalf("seed witness member: %v", err)
			}
			var basisBefore string
			if err := pool.QueryRow(ctx, `SELECT schedule_basis FROM obligation_instances WHERE obligation_id=$1::uuid`, witnessID).Scan(&basisBefore); err != nil {
				t.Fatalf("read witness basis: %v", err)
			}

			v2, r2 := seedContractVersion(t, ctx, pool, f.protoID, 2, fatteningPlanDSL, raceRules(), false)
			before := f.snapshotGoatRows(t, ctx, pool)
			err := f.race(t, ctx, pool, func() error {
				_, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{f.goat, witness}, []string{v2})
				return err
			})
			requireClinicalOrSerializationRejection(t, "carry-over", err)
			f.assertNoWrittenRowBelowFloor(t, ctx, pool, before)
			if err != nil {
				return
			}

			var (
				version, rule, batchAfter, keyAfter, basisAfter string
				members                                         int
			)
			if err := pool.QueryRow(ctx, `
SELECT oi.protocol_version_id::text, oi.rule_id::text, COALESCE(oi.batch_id::text, ''), oi.idempotency_key, oi.schedule_basis,
       (SELECT count(*) FROM vaccination_drive_assignment_members m WHERE m.obligation_id = oi.obligation_id AND m.assignment_id = $2::uuid)
FROM obligation_instances oi WHERE oi.obligation_id = $1::uuid`, witnessID, assignmentID).
				Scan(&version, &rule, &batchAfter, &keyAfter, &basisAfter, &members); err != nil {
				t.Fatalf("read witness after carry-over (obligation_id %s must survive): %v", witnessID, err)
			}
			if version != v2 || rule != r2["et_tt_kid"] {
				t.Fatalf("witness not rebound in place: version=%s rule=%s, want %s/%s", version, rule, v2, r2["et_tt_kid"])
			}
			if batchAfter != batchID || keyAfter != witnessKey || basisAfter != basisBefore || members != 1 {
				t.Fatalf("witness lost identity: batch=%s(%s) key=%s(%s) basis=%s(%s) members=%d",
					batchAfter, batchID, keyAfter, witnessKey, basisAfter, basisBefore, members)
			}
		})
	}
}
