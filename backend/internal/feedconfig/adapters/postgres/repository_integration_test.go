package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Proofs against the REAL schema (migrations 000003 + 000004).
//
// These exist because the fake-backed service and handler tests cannot see SQL. A fake returns
// whatever a test stocked it with, so it will happily "prove" an effective-dated supersede that the
// query never performs, a column that does not exist, or an idempotency index that was never
// created. Everything asserted below goes through the statements production runs.
//
// The three behaviours only a database can prove:
//
//	EFFECTIVE DATING  -- a change CLOSES the old row and OPENS a new one, and the old rate is still
//	                     readable afterwards. A fake cannot show that history survived.
//	SAME-DAY CORRECTION -- a re-edit on the row's own valid_from date corrects in place, because the
//	                     schema's valid_to > valid_from CHECK makes the supersede path impossible.
//	IDEMPOTENCY      -- replay and conflict are decided by a real unique index on
//	                     (tenant_id, idempotency_key), not by an in-memory map.

const (
	fcTenant = "00000000-0000-4000-8000-000000000001"
	fcPark   = "00000000-0000-4000-8000-000000003001"
	fcShed   = "00000000-0000-4000-8000-000000004001"
	// A location that is a SHED, used to prove a shed id passed as park_id is rejected on type rather
	// than sliding through on the foreign key alone.
	fcOtherShed = "00000000-0000-4000-8000-000000004002"
	// A SECOND park, and a shed that belongs to IT (not fcPark). Used to prove a write that supplies
	// a valid park + a valid shed cannot mix the two when the shed does not belong to that park
	// (CR-03).
	fcOtherPark     = "00000000-0000-4000-8000-000000003002"
	fcShedOtherPark = "00000000-0000-4000-8000-000000004003"
)

func setupFeedConfigDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	seedFeedConfigScope(t, ctx, pool)
	return pool
}

func seedFeedConfigScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Mesha Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, fcTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fcTenant, fcPark); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-S1', 'CPT Shed 1', $2::uuid, 'active'),
  ($4::uuid, $1::uuid, 'shed', 'CPT-S2', 'CPT Shed 2', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, fcTenant, fcPark, fcShed, fcOtherShed); err != nil {
		t.Fatalf("seed sheds: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'BLR', 'BLR', 'active')
ON CONFLICT (location_id) DO NOTHING`, fcTenant, fcOtherPark); err != nil {
		t.Fatalf("seed other park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'BLR-S1', 'BLR Shed 1', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, fcTenant, fcOtherPark, fcShedOtherPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}
}

func fcRepo(pool *pgxpool.Pool) *Repository { return NewRepository(pool, 10*time.Second) }

// rateCommand builds a ration-rate command. Each call gets its own key/fingerprint unless the test
// deliberately reuses them.
func rateCommand(key, fingerprint, grams, effectiveFrom string) domain.UpsertRationRateCommand {
	return domain.UpsertRationRateCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: effectiveFrom,
			IdempotencyKey: key, RequestFingerprint: fingerprint,
		},
		ParkID: fcPark, RationGroupLabel: "Boer", ShedTagLabel: "Pregnant",
		FeedItemLabel: "Concentrate", GramsPerHead: grams,
	}
}

// openRate reads the currently-open rate for the fixture cell.
func openRate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (id, grams, validFrom string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT ration_rate_id::text, grams_per_head::text, valid_from::text
FROM feed_ration_rates
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND ration_group_key = feed_config_norm('Boer')
  AND shed_tag_key = feed_config_norm('Pregnant')
  AND feed_item_key = feed_config_norm('Concentrate')
  AND valid_to IS NULL`, fcTenant, fcPark).Scan(&id, &grams, &validFrom); err != nil {
		t.Fatalf("read open rate: %v", err)
	}
	return id, grams, validFrom
}

// TestUpsertRationRateInsertsFirstAuthoredValue covers the "no open row" branch.
func TestUpsertRationRateInsertsFirstAuthoredValue(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	got, err := repo.UpsertRationRate(ctx, rateCommand("key-insert-0001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("UpsertRationRate: %v", err)
	}
	if got.Outcome != domain.OutcomeInserted {
		t.Fatalf("outcome = %q, want %q", got.Outcome, domain.OutcomeInserted)
	}
	if got.Replayed {
		t.Fatalf("a first write reported idempotent_replay=true")
	}
	if got.SupersededRowID != "" {
		t.Fatalf("a first write superseded row %q, want none", got.SupersededRowID)
	}

	id, grams, validFrom := openRate(t, ctx, pool)
	if id != got.ResultRowID || grams != "250.000" || validFrom != "2026-07-19" {
		t.Fatalf("open row = (%s, %s, %s), want (%s, 250.000, 2026-07-19)", id, grams, validFrom, got.ResultRowID)
	}
}

// TestUpsertRationRateAuthoredZeroIsStoredAsZero proves an authored 0 becomes a REAL ROW rather than
// being skipped as "nothing to write".
//
// This is the schema's central safety rule seen from the write side: a stored 0.000 means "feed
// nothing" (correct for milk-fed kids), while NO ROW means "not configured" and must block. If the
// write path had treated 0 as absent, the cell would be left unconfigured and the shed would block
// instead of correctly being fed nothing.
func TestUpsertRationRateAuthoredZeroIsStoredAsZero(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-zero-00001", "fp-zero", "0.000", "2026-07-19")); err != nil {
		t.Fatalf("UpsertRationRate: %v", err)
	}
	_, grams, _ := openRate(t, ctx, pool)
	if grams != "0.000" {
		t.Fatalf("grams_per_head = %q, want an authored 0.000 row", grams)
	}
}

// TestUpsertRationRateExactReplayReturnsOriginalWithoutSideEffects is the idempotency proof.
//
// The same key with the same fingerprint must return the ORIGINAL result and write nothing new.
// The row count assertion is what makes it a real proof: an implementation that merely returned a
// cached answer while still inserting a second row would pass a result-only check.
func TestUpsertRationRateExactReplayReturnsOriginalWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertRationRate(ctx, rateCommand("key-replay-0001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	second, err := repo.UpsertRationRate(ctx, rateCommand("key-replay-0001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !second.Replayed {
		t.Fatalf("replay reported idempotent_replay=false")
	}
	if second.WriteID != first.WriteID || second.ResultRowID != first.ResultRowID || second.Outcome != first.Outcome {
		t.Fatalf("replay returned %+v, want the original %+v", second, first)
	}

	var rateRows, ledgerRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_ration_rates WHERE tenant_id = $1::uuid`, fcTenant).Scan(&rateRows); err != nil {
		t.Fatalf("count rates: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_config_write_log WHERE tenant_id = $1::uuid`, fcTenant).Scan(&ledgerRows); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if rateRows != 1 || ledgerRows != 1 {
		t.Fatalf("after replay: %d rate rows and %d ledger rows, want 1 and 1", rateRows, ledgerRows)
	}
}

// TestUpsertRationRateSameKeyDifferentPayloadIsConflict proves the OTHER half of idempotency.
//
// Reusing a key for a genuinely different edit must FAIL, and must apply nothing. Silently replaying
// the first edit's result would tell the author their new rate was saved when it was not; silently
// applying the second under the first's key would make the key meaningless.
func TestUpsertRationRateSameKeyDifferentPayloadIsConflict(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-conflict-001", "fp-a", "250.000", "2026-07-19")); err != nil {
		t.Fatalf("first write: %v", err)
	}

	_, err := repo.UpsertRationRate(ctx, rateCommand("key-conflict-001", "fp-DIFFERENT", "999.000", "2026-07-19"))
	if err == nil {
		t.Fatalf("same-key/different-payload replay was accepted, want a conflict")
	}
	if err != ports.ErrIdempotencyConflict { //nolint:errorlint // sentinel comparison is the contract here
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}

	// The conflicting edit must have applied NOTHING.
	_, grams, _ := openRate(t, ctx, pool)
	if grams != "250.000" {
		t.Fatalf("open rate = %q after a rejected conflict, want the original 250.000", grams)
	}
	var rateRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_ration_rates WHERE tenant_id = $1::uuid`, fcTenant).Scan(&rateRows); err != nil {
		t.Fatalf("count rates: %v", err)
	}
	if rateRows != 1 {
		t.Fatalf("%d rate rows after a rejected conflict, want 1", rateRows)
	}
}

// TestUpsertRationRateSupersedesRatherThanOverwrites is the effective-dating proof.
//
// A rate change on a LATER business day must close the old row and open a new one, leaving the old
// rate readable as history. An in-place UPDATE would destroy the answer to "what were we feeding
// this cohort last month", which is the whole reason the table is effective-dated.
func TestUpsertRationRateSupersedesRatherThanOverwrites(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertRationRate(ctx, rateCommand("key-super-0001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	second, err := repo.UpsertRationRate(ctx, rateCommand("key-super-0002", "fp-b", "300.000", "2026-07-20"))
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if second.Outcome != domain.OutcomeSuperseded {
		t.Fatalf("outcome = %q, want %q", second.Outcome, domain.OutcomeSuperseded)
	}
	if second.SupersededRowID != first.ResultRowID {
		t.Fatalf("superseded_row_id = %q, want the first row %q", second.SupersededRowID, first.ResultRowID)
	}
	if second.ResultRowID == first.ResultRowID {
		t.Fatalf("supersede reused the original row id; it must open a NEW row")
	}

	// The OLD row must still exist, closed on the day the change took effect.
	var oldGrams, oldValidFrom, oldValidTo string
	if err := pool.QueryRow(ctx, `
SELECT grams_per_head::text, valid_from::text, valid_to::text
FROM feed_ration_rates WHERE ration_rate_id = $1::uuid`, first.ResultRowID).
		Scan(&oldGrams, &oldValidFrom, &oldValidTo); err != nil {
		t.Fatalf("read superseded row: %v", err)
	}
	if oldGrams != "250.000" {
		t.Fatalf("superseded row grams = %q, want the historical 250.000 preserved", oldGrams)
	}
	if oldValidFrom != "2026-07-19" || oldValidTo != "2026-07-20" {
		t.Fatalf("superseded window = [%s, %s), want [2026-07-19, 2026-07-20)", oldValidFrom, oldValidTo)
	}

	// And exactly ONE row is open, carrying the new rate.
	id, grams, validFrom := openRate(t, ctx, pool)
	if id != second.ResultRowID || grams != "300.000" || validFrom != "2026-07-20" {
		t.Fatalf("open row = (%s, %s, %s), want (%s, 300.000, 2026-07-20)", id, grams, validFrom, second.ResultRowID)
	}
}

// TestUpsertRationRateSameDayEditCorrectsInPlace covers the branch the schema FORCES.
//
// A window closed on the day it opened cannot satisfy valid_to > valid_from, so a same-business-day
// re-author has no legal supersede. It is a correction of today's authoring, not a historical
// change, and must update the open row in place while leaving exactly one row behind.
func TestUpsertRationRateSameDayEditCorrectsInPlace(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertRationRate(ctx, rateCommand("key-sameday-001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	second, err := repo.UpsertRationRate(ctx, rateCommand("key-sameday-002", "fp-b", "260.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("same-day correction: %v", err)
	}
	if second.Outcome != domain.OutcomeCorrected {
		t.Fatalf("outcome = %q, want %q", second.Outcome, domain.OutcomeCorrected)
	}
	if second.ResultRowID != first.ResultRowID {
		t.Fatalf("correction opened a new row %q; a same-day re-author must correct row %q in place",
			second.ResultRowID, first.ResultRowID)
	}
	if second.SupersededRowID != "" {
		t.Fatalf("correction reported superseded_row_id=%q, want none", second.SupersededRowID)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_ration_rates WHERE tenant_id = $1::uuid`, fcTenant).Scan(&rows); err != nil {
		t.Fatalf("count rates: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d rate rows after a same-day correction, want 1", rows)
	}
	_, grams, validFrom := openRate(t, ctx, pool)
	if grams != "260.000" || validFrom != "2026-07-19" {
		t.Fatalf("corrected row = (%s, %s), want (260.000, 2026-07-19)", grams, validFrom)
	}
}

// TestUpsertRationRateUnchangedWritesNothing proves re-authoring the identical value opens no empty
// window. Recording it as a change would litter the history with zero-difference rows and make the
// audit trail unreadable.
func TestUpsertRationRateUnchangedWritesNothing(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertRationRate(ctx, rateCommand("key-nochange-01", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	// A LATER day, a DIFFERENT key, but the same value.
	second, err := repo.UpsertRationRate(ctx, rateCommand("key-nochange-02", "fp-b", "250.000", "2026-07-25"))
	if err != nil {
		t.Fatalf("no-op write: %v", err)
	}
	if second.Outcome != domain.OutcomeUnchanged {
		t.Fatalf("outcome = %q, want %q", second.Outcome, domain.OutcomeUnchanged)
	}
	if second.ResultRowID != first.ResultRowID {
		t.Fatalf("unchanged write pointed at %q, want the still-open row %q", second.ResultRowID, first.ResultRowID)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_ration_rates WHERE tenant_id = $1::uuid`, fcTenant).Scan(&rows); err != nil {
		t.Fatalf("count rates: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d rate rows after a no-op write, want 1", rows)
	}
	// The ledger still records the attempt, so the audit trail shows someone re-authored it.
	var ledger int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_config_write_log WHERE tenant_id = $1::uuid`, fcTenant).Scan(&ledger); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledger != 2 {
		t.Fatalf("%d ledger rows, want 2 (both authored attempts recorded)", ledger)
	}
}

// TestUpsertRationRateNormalizesLabelVariants proves the join key really is the database's
// feed_config_norm and not raw string equality.
//
// "Pregnant" and " pregnant " are the SAME cell. Without normalization the second edit would open a
// second, competing open row for one logical rate and the feed lookup would become non-deterministic.
func TestUpsertRationRateNormalizesLabelVariants(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-norm-00001", "fp-a", "250.000", "2026-07-19")); err != nil {
		t.Fatalf("first write: %v", err)
	}

	variant := rateCommand("key-norm-00002", "fp-b", "300.000", "2026-07-20")
	variant.ShedTagLabel = "pregnant" // case variant of the same tag
	got, err := repo.UpsertRationRate(ctx, variant)
	if err != nil {
		t.Fatalf("variant write: %v", err)
	}
	if got.Outcome != domain.OutcomeSuperseded {
		t.Fatalf("outcome = %q, want %q -- the case variant must resolve to the SAME cell", got.Outcome, domain.OutcomeSuperseded)
	}

	var open int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_ration_rates
WHERE tenant_id = $1::uuid AND valid_to IS NULL`, fcTenant).Scan(&open); err != nil {
		t.Fatalf("count open rates: %v", err)
	}
	if open != 1 {
		t.Fatalf("%d open rows after a label case variant, want exactly 1", open)
	}
}

// TestUpsertRationRateRejectsNonParkLocation proves the type check does work the foreign key cannot.
// A shed id satisfies the FK, so without this check a rate would be authored under a scope the feed
// direction path never reads -- a silent success that looks exactly like a saved edit.
func TestUpsertRationRateRejectsNonParkLocation(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	cmd := rateCommand("key-badpark-001", "fp-a", "250.000", "2026-07-19")
	cmd.ParkID = fcShed                                                          // a real location, but a SHED
	if _, err := repo.UpsertRationRate(ctx, cmd); err != ports.ErrParkNotFound { //nolint:errorlint // sentinel comparison is the contract here
		t.Fatalf("error = %v, want ErrParkNotFound", err)
	}
}

// TestUpsertScheduleConfigLifecycle exercises the dispatch clock through all four outcomes and pins
// the local-time storage semantics.
func TestUpsertScheduleConfigLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	transport := "15:45:00"
	base := domain.UpsertScheduleConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-sched-0001", RequestFingerprint: "fp-a",
		},
		ParkID: fcPark, Workflow: domain.WorkflowNormal,
		DirectionTime: "07:00:00", CorrectionTime: "14:00:00", TransportTime: &transport,
	}

	first, err := repo.UpsertScheduleConfig(ctx, base)
	if err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	if first.Outcome != domain.OutcomeInserted {
		t.Fatalf("outcome = %q, want inserted", first.Outcome)
	}

	// Stored as LOCAL wall-clock with no offset applied. A timezone conversion anywhere in the write
	// path would show up here as 01:30:00.
	var direction, correction, storedTransport string
	if err := pool.QueryRow(ctx, `
SELECT direction_time::text, correction_time::text, transport_time::text
FROM feed_schedule_config WHERE feed_schedule_config_id = $1::uuid`, first.ResultRowID).
		Scan(&direction, &correction, &storedTransport); err != nil {
		t.Fatalf("read schedule: %v", err)
	}
	if direction != "07:00:00" || correction != "14:00:00" || storedTransport != "15:45:00" {
		t.Fatalf("stored clock = (%s, %s, %s), want the local (07:00:00, 14:00:00, 15:45:00) with no offset applied",
			direction, correction, storedTransport)
	}

	// Exact replay.
	replay, err := repo.UpsertScheduleConfig(ctx, base)
	if err != nil {
		t.Fatalf("replay schedule: %v", err)
	}
	if !replay.Replayed || replay.WriteID != first.WriteID {
		t.Fatalf("replay = %+v, want the original %+v with idempotent_replay=true", replay, first)
	}

	// Same-key/different-payload conflict.
	conflicting := base
	conflicting.RequestFingerprint = "fp-DIFFERENT"
	conflicting.DirectionTime = "08:00:00"
	if _, err := repo.UpsertScheduleConfig(ctx, conflicting); err != ports.ErrIdempotencyConflict { //nolint:errorlint // sentinel comparison is the contract here
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}

	// A change on a LATER day supersedes.
	changed := base
	changed.IdempotencyKey = "key-sched-0002"
	changed.RequestFingerprint = "fp-b"
	changed.EffectiveFrom = "2026-07-20"
	changed.DirectionTime = "06:30:00"
	superseded, err := repo.UpsertScheduleConfig(ctx, changed)
	if err != nil {
		t.Fatalf("supersede schedule: %v", err)
	}
	if superseded.Outcome != domain.OutcomeSuperseded || superseded.SupersededRowID != first.ResultRowID {
		t.Fatalf("outcome/superseded = %q/%q, want superseded/%q",
			superseded.Outcome, superseded.SupersededRowID, first.ResultRowID)
	}

	// Changing ONLY the transport cutoff must still count as a change. Comparing just the two NOT
	// NULL times would report "unchanged" for an edit the author really made.
	newTransport := "16:30:00"
	transportOnly := changed
	transportOnly.IdempotencyKey = "key-sched-0003"
	transportOnly.RequestFingerprint = "fp-c"
	transportOnly.EffectiveFrom = "2026-07-21"
	transportOnly.TransportTime = &newTransport
	transportChange, err := repo.UpsertScheduleConfig(ctx, transportOnly)
	if err != nil {
		t.Fatalf("transport-only change: %v", err)
	}
	if transportChange.Outcome != domain.OutcomeSuperseded {
		t.Fatalf("transport-only outcome = %q, want superseded", transportChange.Outcome)
	}

	// Re-authoring the identical clock writes nothing.
	noop := transportOnly
	noop.IdempotencyKey = "key-sched-0004"
	noop.RequestFingerprint = "fp-d"
	noop.EffectiveFrom = "2026-07-22"
	unchanged, err := repo.UpsertScheduleConfig(ctx, noop)
	if err != nil {
		t.Fatalf("no-op schedule write: %v", err)
	}
	if unchanged.Outcome != domain.OutcomeUnchanged {
		t.Fatalf("outcome = %q, want unchanged", unchanged.Outcome)
	}
}

// TestScheduleWorkflowsAreIndependent proves the per-workflow key really is per-workflow: authoring
// the experiment clock must not disturb the normal one. If workflow were not part of the natural
// key, the second write would supersede the first and one of the two would be dispatched at the
// wrong hour every day.
func TestScheduleWorkflowsAreIndependent(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	transport := "15:45:00"
	normal := domain.UpsertScheduleConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-wf-normal01", RequestFingerprint: "fp-n",
		},
		ParkID: fcPark, Workflow: domain.WorkflowNormal,
		DirectionTime: "07:00:00", CorrectionTime: "14:00:00", TransportTime: &transport,
	}
	experiment := normal
	experiment.Workflow = domain.WorkflowExperiment
	experiment.DirectionTime = "14:00:00" // the live experiment clock: direction == correction
	experiment.IdempotencyKey = "key-wf-experi01"
	experiment.RequestFingerprint = "fp-e"

	if _, err := repo.UpsertScheduleConfig(ctx, normal); err != nil {
		t.Fatalf("normal clock: %v", err)
	}
	if _, err := repo.UpsertScheduleConfig(ctx, experiment); err != nil {
		t.Fatalf("experiment clock: %v", err)
	}

	page, err := repo.ListScheduleConfig(ctx, domain.ScheduleConfigQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("ListScheduleConfig: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("%d in-force clocks, want 2 (one per workflow)", len(page.Items))
	}
	byWorkflow := map[string]domain.ScheduleConfig{}
	for _, item := range page.Items {
		byWorkflow[item.Workflow] = item
	}
	if byWorkflow[domain.WorkflowNormal].DirectionTime != "07:00:00" {
		t.Fatalf("normal direction_time = %q, want 07:00:00", byWorkflow[domain.WorkflowNormal].DirectionTime)
	}
	if byWorkflow[domain.WorkflowExperiment].DirectionTime != "14:00:00" {
		t.Fatalf("experiment direction_time = %q, want 14:00:00", byWorkflow[domain.WorkflowExperiment].DirectionTime)
	}

	// A workflow filter must actually narrow.
	filtered, err := repo.ListScheduleConfig(ctx, domain.ScheduleConfigQuery{
		TenantID: fcTenant, ParkID: fcPark, Workflow: domain.WorkflowExperiment, Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("filtered ListScheduleConfig: %v", err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].Workflow != domain.WorkflowExperiment {
		t.Fatalf("workflow filter returned %d items, want only the experiment clock", len(filtered.Items))
	}
}

// TestScheduleTransportTimeStaysNullWhenAbsent proves an undeclared cutoff is stored and returned as
// NULL rather than being filled in with a plausible-looking default. A fabricated deadline would
// make a late correction look deliverable.
func TestScheduleTransportTimeStaysNullWhenAbsent(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertScheduleConfig(ctx, domain.UpsertScheduleConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-notransp01", RequestFingerprint: "fp-a",
		},
		ParkID: fcPark, Workflow: domain.WorkflowNormal,
		DirectionTime: "07:00:00", CorrectionTime: "14:00:00", TransportTime: nil,
	}); err != nil {
		t.Fatalf("UpsertScheduleConfig: %v", err)
	}

	page, err := repo.ListScheduleConfig(ctx, domain.ScheduleConfigQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("ListScheduleConfig: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("%d clocks, want 1", len(page.Items))
	}
	if page.Items[0].TransportTime != nil {
		t.Fatalf("transport_time = %q, want nil (undeclared cutoff)", *page.Items[0].TransportTime)
	}
}

// TestUpsertShedFactorLifecycle covers the shed multiplier's insert / supersede / same-day paths and
// the shed type check.
func TestUpsertShedFactorLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	cmd := domain.UpsertShedFactorCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-factor-001", RequestFingerprint: "fp-a",
		},
		ParkID: fcPark, ShedID: fcShed, FeedItemLabel: "Concentrate", Multiplier: "1.5000",
	}

	first, err := repo.UpsertShedFactor(ctx, cmd)
	if err != nil {
		t.Fatalf("insert shed factor: %v", err)
	}
	if first.Outcome != domain.OutcomeInserted {
		t.Fatalf("outcome = %q, want inserted", first.Outcome)
	}

	// Same-day correction.
	sameDay := cmd
	sameDay.IdempotencyKey = "key-factor-002"
	sameDay.RequestFingerprint = "fp-b"
	sameDay.Multiplier = "1.2500"
	corrected, err := repo.UpsertShedFactor(ctx, sameDay)
	if err != nil {
		t.Fatalf("same-day correction: %v", err)
	}
	if corrected.Outcome != domain.OutcomeCorrected || corrected.ResultRowID != first.ResultRowID {
		t.Fatalf("outcome/row = %q/%q, want corrected/%q", corrected.Outcome, corrected.ResultRowID, first.ResultRowID)
	}

	// Later-day supersede.
	later := cmd
	later.IdempotencyKey = "key-factor-003"
	later.RequestFingerprint = "fp-c"
	later.EffectiveFrom = "2026-07-20"
	later.Multiplier = "0.0000" // an explicitly authored zero: this shed is deliberately fed none.
	superseded, err := repo.UpsertShedFactor(ctx, later)
	if err != nil {
		t.Fatalf("supersede shed factor: %v", err)
	}
	if superseded.Outcome != domain.OutcomeSuperseded || superseded.SupersededRowID != first.ResultRowID {
		t.Fatalf("outcome/superseded = %q/%q, want superseded/%q",
			superseded.Outcome, superseded.SupersededRowID, first.ResultRowID)
	}

	page, err := repo.ListShedFactors(ctx, domain.ShedFactorQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("ListShedFactors: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Multiplier != "0.0000" {
		t.Fatalf("in-force factors = %+v, want a single authored 0.0000", page.Items)
	}

	// A park id passed as shed_id must be rejected on type.
	badShed := cmd
	badShed.IdempotencyKey = "key-factor-004"
	badShed.RequestFingerprint = "fp-d"
	badShed.ShedID = fcPark
	if _, err := repo.UpsertShedFactor(ctx, badShed); err != ports.ErrShedNotFound { //nolint:errorlint // sentinel comparison is the contract here
		t.Fatalf("error = %v, want ErrShedNotFound", err)
	}
}

// TestFutureDatedOpenRowFailsClosed covers the branch where neither effective-dating path is
// correct. Closing a future-dated row with today's date would violate valid_to > valid_from, and
// correcting it in place would rewrite a future authoring with today's intent -- so the write fails
// and a human decides.
func TestFutureDatedOpenRowFailsClosed(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-future-0001", "fp-a", "250.000", "2026-07-25")); err != nil {
		t.Fatalf("future-dated write: %v", err)
	}
	// An edit dated EARLIER than the open row.
	_, err := repo.UpsertRationRate(ctx, rateCommand("key-future-0002", "fp-b", "300.000", "2026-07-19"))
	if err != ports.ErrFutureDatedRow { //nolint:errorlint // sentinel comparison is the contract here
		t.Fatalf("error = %v, want ErrFutureDatedRow", err)
	}
}

// TestListRationRatesPagesAndFilters proves has_more comes from a real Limit+1 fetch and that the
// filters narrow through the normalized keys rather than by raw equality.
func TestListRationRatesPagesAndFilters(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	fcSeedCatalog(t, ctx, pool, "Concentrate", "Green Fodder")
	cells := []struct{ group, tag, item, grams string }{
		{"Boer", "Pregnant", "Concentrate", "250.000"},
		{"Boer", "Pregnant", "Green Fodder", "1000.000"},
		{"Boer", "Lactating", "Concentrate", "300.000"},
		{"Sojat", "Pregnant", "Concentrate", "220.000"},
	}
	for i, c := range cells {
		cmd := rateCommand("key-page-"+string(rune('a'+i))+"00001", "fp-"+c.item+c.tag+c.group, c.grams, "2026-07-19")
		cmd.RationGroupLabel, cmd.ShedTagLabel, cmd.FeedItemLabel = c.group, c.tag, c.item
		if _, err := repo.UpsertRationRate(ctx, cmd); err != nil {
			t.Fatalf("seed rate %d: %v", i, err)
		}
	}

	// A page smaller than the result set must report has_more.
	firstPage, err := repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 2, Offset: 0},
	})
	if err != nil {
		t.Fatalf("ListRationRates: %v", err)
	}
	if len(firstPage.Items) != 2 || !firstPage.HasMore {
		t.Fatalf("first page = %d items, has_more=%v; want 2 and true", len(firstPage.Items), firstPage.HasMore)
	}

	// The last page must NOT claim more.
	lastPage, err := repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 2, Offset: 2},
	})
	if err != nil {
		t.Fatalf("ListRationRates: %v", err)
	}
	if len(lastPage.Items) != 2 || lastPage.HasMore {
		t.Fatalf("last page = %d items, has_more=%v; want 2 and false", len(lastPage.Items), lastPage.HasMore)
	}

	// A filter expressed with different casing must still match, because the predicate goes through
	// feed_config_norm on both sides.
	filtered, err := repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID: fcTenant, ParkID: fcPark, RationGroup: "boer", ShedTag: "PREGNANT",
		Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("filtered ListRationRates: %v", err)
	}
	if len(filtered.Items) != 2 {
		t.Fatalf("case-variant filter returned %d rows, want 2", len(filtered.Items))
	}
}

// TestListRationRatesBreedItemSetAndGramsFilters proves the three grid filters against the real
// schema. None of them can be proved anywhere else:
//
//	BREED       -- resolves through feed_ration_groups, a table the fake repo does not have. The
//	               mapping is MANY-TO-ONE, so the whole point (Sirohi finds the Beetal/Sirohi rows)
//	               only exists once that join runs.
//	ITEM SET    -- `= ANY (SELECT feed_config_norm(item) FROM unnest($5::text[]))` is SQL. A Go-level
//	               test can show the slice was carried; only Postgres can show it MATCHES, and that
//	               casing still normalizes through the array.
//	GRAMS       -- the CASE comparison runs in numeric. A float-compared 149.995 is exactly the
//	               defect the decimal strings exist to prevent, and only the database shows it.
func TestListRationRatesBreedItemSetAndGramsFilters(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	// The breed -> ration-group map, exactly as the live tenant carries it: two breeds share one
	// group, and the "Kid" group is reachable from NO breed.
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label)
VALUES ($1::uuid, 'Beetal', 'Beetal/Sirohi'),
       ($1::uuid, 'Sirohi', 'Beetal/Sirohi'),
       ($1::uuid, 'Boer', 'Boer')
ON CONFLICT DO NOTHING`, fcTenant); err != nil {
		t.Fatalf("seed ration groups: %v", err)
	}
	fcSeedCatalog(t, ctx, pool, "Concentrate", "Green Fodder", "Baking Soda")

	cells := []struct{ group, tag, item, grams string }{
		{"Beetal/Sirohi", "Pregnant", "Concentrate", "250.000"},
		{"Beetal/Sirohi", "Pregnant", "Green Fodder", "1000.000"},
		{"Beetal/Sirohi", "Pregnant", "Baking Soda", "0.000"},
		{"Boer", "Pregnant", "Concentrate", "300.000"},
		// A kid rate. No breed maps to this group, so every breed filter must exclude it.
		{"Kid", "Pregnant", "Concentrate", "149.995"},
	}
	for i, c := range cells {
		cmd := rateCommand("key-filt-"+string(rune('a'+i))+"00001", "fp-filt-"+c.group+c.item, c.grams, "2026-07-19")
		cmd.RationGroupLabel, cmd.ShedTagLabel, cmd.FeedItemLabel = c.group, c.tag, c.item
		if _, err := repo.UpsertRationRate(ctx, cmd); err != nil {
			t.Fatalf("seed rate %d: %v", i, err)
		}
	}

	list := func(t *testing.T, q domain.RationRateQuery) []domain.RationRate {
		t.Helper()
		q.TenantID, q.ParkID = fcTenant, fcPark
		q.Page = domain.Page{Limit: 50}
		page, err := repo.ListRationRates(ctx, q)
		if err != nil {
			t.Fatalf("ListRationRates(%+v): %v", q, err)
		}
		return page.Items
	}

	t.Run("a breed finds the rows of the group it resolves into", func(t *testing.T) {
		// Sirohi is not the name of any group. Without the map this returns nothing.
		got := list(t, domain.RationRateQuery{Breed: "Sirohi"})
		if len(got) != 3 {
			t.Fatalf("Sirohi returned %d rows, want the 3 Beetal/Sirohi rows", len(got))
		}
		for _, row := range got {
			if row.RationGroupLabel != "Beetal/Sirohi" {
				t.Fatalf("Sirohi returned a %q row", row.RationGroupLabel)
			}
		}
		// Its co-breed must resolve to the same set: that is what many-to-one means.
		if other := list(t, domain.RationRateQuery{Breed: "Beetal"}); len(other) != len(got) {
			t.Fatalf("Beetal returned %d rows and Sirohi %d; the two share one group", len(other), len(got))
		}
	})

	t.Run("a breed excludes kid rates", func(t *testing.T) {
		for _, row := range list(t, domain.RationRateQuery{Breed: "Boer"}) {
			if row.RationGroupLabel == "Kid" {
				t.Fatal("a breed filter returned a Kid rate — kid rates are not breed-specific")
			}
		}
	})

	t.Run("an unknown breed narrows to nothing, never widens to everything", func(t *testing.T) {
		// The failure this guards is silent and total: a NULL from the subquery that widened the
		// predicate instead of failing it would return the WHOLE park's grid to someone who asked
		// for one breed.
		if got := list(t, domain.RationRateQuery{Breed: "Nonesuch"}); len(got) != 0 {
			t.Fatalf("unknown breed returned %d rows, want 0", len(got))
		}
	})

	t.Run("breed matching is case- and separator-insensitive", func(t *testing.T) {
		if got := list(t, domain.RationRateQuery{Breed: "  sirohi "}); len(got) != 3 {
			t.Fatalf("case-variant breed returned %d rows, want 3", len(got))
		}
	})

	t.Run("a feed-item set matches any of its members", func(t *testing.T) {
		got := list(t, domain.RationRateQuery{FeedItems: []string{"Concentrate", "Green Fodder"}})
		if len(got) != 4 {
			t.Fatalf("two-item set returned %d rows, want 4", len(got))
		}
		for _, row := range got {
			if row.FeedItemLabel != "Concentrate" && row.FeedItemLabel != "Green Fodder" {
				t.Fatalf("item set returned an unrelated %q row", row.FeedItemLabel)
			}
		}
		// One member is still a set of one, and the normalization must survive the array.
		if one := list(t, domain.RationRateQuery{FeedItems: []string{"  GREEN fodder"}}); len(one) != 1 {
			t.Fatalf("case-variant single item returned %d rows, want 1", len(one))
		}
	})

	t.Run("no feed-item set is no filter", func(t *testing.T) {
		if got := list(t, domain.RationRateQuery{FeedItems: nil}); len(got) != len(cells) {
			t.Fatalf("nil item set returned %d rows, want all %d", len(got), len(cells))
		}
	})

	t.Run("more than zero hides the authored zeros", func(t *testing.T) {
		got := list(t, domain.RationRateQuery{GramsCompare: &domain.GramsComparison{Op: domain.GramsOpGreaterThan, Value: "0"}})
		if len(got) != 4 {
			t.Fatalf("grams > 0 returned %d rows, want 4", len(got))
		}
		for _, row := range got {
			if row.GramsPerHead == "0.000" {
				t.Fatal("grams > 0 returned an authored zero")
			}
		}
	})

	t.Run("exactly zero isolates the authored zeros", func(t *testing.T) {
		got := list(t, domain.RationRateQuery{GramsCompare: &domain.GramsComparison{Op: domain.GramsOpEquals, Value: "0"}})
		if len(got) != 1 || got[0].FeedItemLabel != "Baking Soda" {
			t.Fatalf("grams = 0 returned %+v, want only the Baking Soda zero", got)
		}
	})

	t.Run("comparison is exact decimal, not float", func(t *testing.T) {
		// 149.995 is the kid rate. A float round-trip is what makes an equality like this miss.
		got := list(t, domain.RationRateQuery{GramsCompare: &domain.GramsComparison{Op: domain.GramsOpEquals, Value: "149.995"}})
		if len(got) != 1 {
			t.Fatalf("grams = 149.995 returned %d rows, want exactly the row authored at that rate", len(got))
		}
		if got[0].GramsPerHead != "149.995" {
			t.Fatalf("grams = %q, want the exact authored decimal 149.995", got[0].GramsPerHead)
		}
	})

	t.Run("every operator is wired to the comparison it names", func(t *testing.T) {
		// One CASE arm per operator, and a mis-wired arm (gte behaving as gt, lt as lte) is invisible
		// in any test that only exercises the common one.
		for _, tc := range []struct {
			op   domain.GramsOp
			want int
		}{
			{domain.GramsOpGreaterThan, 2}, // 300, 1000
			{domain.GramsOpAtLeast, 3},     // + 250
			{domain.GramsOpEquals, 1},      // 250
			{domain.GramsOpAtMost, 3},      // 250, 149.995, 0
			{domain.GramsOpLessThan, 2},    // 149.995, 0
			{domain.GramsOpNotEqualTo, 4},  // everything but 250
		} {
			got := list(t, domain.RationRateQuery{GramsCompare: &domain.GramsComparison{Op: tc.op, Value: "250"}})
			if len(got) != tc.want {
				t.Errorf("grams %s 250 returned %d rows, want %d", tc.op, len(got), tc.want)
			}
		}
	})

	t.Run("filters compose rather than replace one another", func(t *testing.T) {
		got := list(t, domain.RationRateQuery{
			Breed:        "Sirohi",
			FeedItems:    []string{"Concentrate", "Baking Soda"},
			GramsCompare: &domain.GramsComparison{Op: domain.GramsOpGreaterThan, Value: "0"},
		})
		if len(got) != 1 || got[0].FeedItemLabel != "Concentrate" {
			t.Fatalf("composed filter returned %+v, want only the Beetal/Sirohi Concentrate row", got)
		}
	})
}

// TestListExperimentConfigFiltersKeepPenPagingHonest proves the experiment filters against the real
// schema, and specifically the thing that can only go wrong HERE.
//
// This read pages by PEN, not by cell: it ranks distinct pens in a CTE, takes a window of them, and
// then joins their cells back. So every cell-level filter has to be applied in BOTH places. Apply
// it only to the ranking and the page returns cells nobody asked for; apply it only to the join and
// the pen COUNT (and therefore has_more, and which pens land on which page) is computed over pens
// that contribute no rows -- a page that renders short, or empty, with a pager that insists there
// is more. The two predicates are one shared const in the query for exactly this reason, and this
// test is what proves the sharing actually holds.
func TestListExperimentConfigFiltersKeepPenPagingHonest(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	// The pen CATALOG. An authored pen label is validated against it, and its normalization strips a
	// leading "part " -- a different vocabulary from feed_experiment_config.partition_key, so it is
	// seeded the catalog's own way rather than with feed_config_norm.
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source)
SELECT $1::uuid, $2::uuid, label, regexp_replace(lower(btrim(label)), '^part[[:space:]]+', ''), 'manual'
FROM unnest(ARRAY['Part 1','Part 2','Part 3']) AS t(label)
ON CONFLICT DO NOTHING`, fcTenant, fcShed); err != nil {
		t.Fatalf("seed shed partitions: %v", err)
	}

	// Three pens of one shed, each enrolled ATOMICALLY -- membership is the workflow flag, so a pen
	// exists only once its complete set of cells is authored in one write. Only ONE pen holds
	// Concentrate, so a Concentrate filter must reduce the pen count from three to one, not merely
	// hide rows inside three pens.
	pens := []struct {
		partition, category string
		cells               []domain.ExperimentBatchCell
	}{
		{"Part 1", "Sheep M NEW", []domain.ExperimentBatchCell{
			{FeedItemLabel: "Concentrate", GramsPerHead: "14.000"},
			{FeedItemLabel: "Green Fodder", GramsPerHead: "4.000"},
			{FeedItemLabel: "Baking Soda", GramsPerHead: "0.000"},
		}},
		{"Part 2", "B+S Goat F NEW", []domain.ExperimentBatchCell{
			{FeedItemLabel: "Green Fodder", GramsPerHead: "6.000"},
		}},
		{"Part 3", "Sheep M NEW", []domain.ExperimentBatchCell{
			{FeedItemLabel: "Green Fodder", GramsPerHead: "8.000"},
		}},
	}
	for i, p := range pens {
		key := "key-expf-" + string(rune('a'+i)) + "00001"
		if _, err := repo.UpsertExperimentConfigBatch(ctx, domain.UpsertExperimentConfigBatchCommand{
			WriteIdentity: domain.WriteIdentity{
				TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-20",
				IdempotencyKey: key, RequestFingerprint: "fp-" + key,
			},
			ParkID: fcPark, ShedID: fcShed, PartitionLabel: p.partition,
			ExperimentCategory: p.category, Cells: p.cells,
		}); err != nil {
			t.Fatalf("enroll experiment pen %d: %v", i, err)
		}
	}

	list := func(t *testing.T, q domain.ExperimentConfigQuery) domain.ExperimentConfigPage {
		t.Helper()
		q.TenantID = fcTenant
		if q.Page.Limit == 0 {
			q.Page.Limit = 50
		}
		page, err := repo.ListExperimentConfig(ctx, q)
		if err != nil {
			t.Fatalf("ListExperimentConfig(%+v): %v", q, err)
		}
		return page
	}

	distinctPens := func(page domain.ExperimentConfigPage) map[string]bool {
		out := map[string]bool{}
		for _, row := range page.Items {
			out[row.ShedID+"#"+row.PartitionLabel] = true
		}
		return out
	}

	t.Run("unfiltered shows every pen", func(t *testing.T) {
		if got := distinctPens(list(t, domain.ExperimentConfigQuery{})); len(got) != 3 {
			t.Fatalf("unfiltered returned %d pens, want 3", len(got))
		}
	})

	t.Run("a feed-item filter drops pens that do not hold it", func(t *testing.T) {
		page := list(t, domain.ExperimentConfigQuery{FeedItems: []string{"Concentrate"}})
		if pensSeen := distinctPens(page); len(pensSeen) != 1 {
			t.Fatalf("Concentrate returned %d pens, want 1 — the pen-ranking CTE is not filtered", len(pensSeen))
		}
		for _, row := range page.Items {
			if row.FeedItemLabel != "Concentrate" {
				t.Fatalf("Concentrate filter returned a %q cell — the outer join is not filtered", row.FeedItemLabel)
			}
		}
	})

	t.Run("a feed-item set matches any member", func(t *testing.T) {
		page := list(t, domain.ExperimentConfigQuery{FeedItems: []string{"Concentrate", "Baking Soda"}})
		if len(page.Items) != 2 {
			t.Fatalf("two-item set returned %d cells, want 2", len(page.Items))
		}
	})

	t.Run("an arm filter narrows to that arm's pens", func(t *testing.T) {
		page := list(t, domain.ExperimentConfigQuery{ExperimentCategory: "b+s goat f new"})
		if pensSeen := distinctPens(page); len(pensSeen) != 1 {
			t.Fatalf("arm filter returned %d pens, want 1 (and it must be case-insensitive)", len(pensSeen))
		}
	})

	t.Run("more than zero hides the authored zeros", func(t *testing.T) {
		page := list(t, domain.ExperimentConfigQuery{GramsCompare: &domain.GramsComparison{Op: domain.GramsOpGreaterThan, Value: "0"}})
		for _, row := range page.Items {
			if row.GramsPerHead == "0.000" {
				t.Fatal("kg > 0 returned an authored zero")
			}
		}
		if len(page.Items) != 4 {
			t.Fatalf("kg > 0 returned %d cells, want 4", len(page.Items))
		}
	})

	t.Run("has_more and the page window agree with the filter", func(t *testing.T) {
		// The real trap: one pen matches, so a one-pen page must NOT claim there is another. A pen
		// count computed before the filter would say 3 > 1 and set has_more.
		page := list(t, domain.ExperimentConfigQuery{
			FeedItems: []string{"Concentrate"},
			Page:      domain.Page{Limit: 1},
		})
		if page.HasMore {
			t.Fatal("has_more is true for a filter matching exactly one pen — the pen count ignores the cell filter")
		}
		// And with the filter off, one pen of three must report more.
		if unfiltered := list(t, domain.ExperimentConfigQuery{Page: domain.Page{Limit: 1}}); !unfiltered.HasMore {
			t.Fatal("has_more is false for page 1 of 3 pens")
		}
	})

	t.Run("filters compose", func(t *testing.T) {
		page := list(t, domain.ExperimentConfigQuery{
			ExperimentCategory: "Sheep M NEW",
			FeedItems:          []string{"Green Fodder"},
			GramsCompare:       &domain.GramsComparison{Op: domain.GramsOpAtLeast, Value: "8"},
		})
		if len(page.Items) != 1 || page.Items[0].GramsPerHead != "8.000" {
			t.Fatalf("composed filter returned %+v, want only the 8.000 Green Fodder cell", page.Items)
		}
	})
}

// TestListsExcludeSupersededRows proves the edit screen shows only what is IN FORCE. Mixing closed
// historical windows into the grid would present superseded rates as editable current values.
func TestListsExcludeSupersededRows(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	fcSeedCatalog(t, ctx, pool, "Concentrate")
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-hist-00001", "fp-a", "250.000", "2026-07-19")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-hist-00002", "fp-b", "300.000", "2026-07-20")); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	page, err := repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID: fcTenant, ParkID: fcPark, Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("ListRationRates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].GramsPerHead != "300.000" {
		t.Fatalf("listing returned %+v, want only the in-force 300.000 row", page.Items)
	}

	// The history is still in the table -- it is excluded from the listing, not deleted.
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_ration_rates WHERE tenant_id = $1::uuid`, fcTenant).Scan(&total); err != nil {
		t.Fatalf("count rates: %v", err)
	}
	if total != 2 {
		t.Fatalf("%d stored rate rows, want 2 (the closed row must be preserved)", total)
	}
}

// TestWriteLogRecordsAuditTrail proves the ledger captures who/what/when alongside the effect, in the
// same transaction. Without it, an effective-dated supersede would have no record of the actor at
// all -- the config tables' created_by cannot express which write closed a row.
func TestWriteLogRecordsAuditTrail(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertRationRate(ctx, rateCommand("key-audit-0001", "fp-a", "250.000", "2026-07-19"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	second, err := repo.UpsertRationRate(ctx, rateCommand("key-audit-0002", "fp-b", "300.000", "2026-07-20"))
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}

	var kind, outcome, actor, effectiveFrom, resultRow, supersededRow string
	if err := pool.QueryRow(ctx, `
SELECT write_kind, outcome, actor_ref, effective_from::text,
       result_row_id::text, superseded_row_id::text
FROM feed_config_write_log
WHERE tenant_id = $1::uuid AND idempotency_key = 'key-audit-0002'`, fcTenant).
		Scan(&kind, &outcome, &actor, &effectiveFrom, &resultRow, &supersededRow); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if kind != domain.WriteKindRationRate || outcome != domain.OutcomeSuperseded || actor != "tester" {
		t.Fatalf("ledger = (%s, %s, %s), want (ration_rate, superseded, tester)", kind, outcome, actor)
	}
	if effectiveFrom != "2026-07-20" {
		t.Fatalf("ledger effective_from = %q, want 2026-07-20", effectiveFrom)
	}
	// Both ends of the window it created must be recorded, or the history it claims to preserve
	// cannot be walked from the ledger.
	if resultRow != second.ResultRowID || supersededRow != first.ResultRowID {
		t.Fatalf("ledger rows = (%s, %s), want (%s, %s)", resultRow, supersededRow, second.ResultRowID, first.ResultRowID)
	}
}

// experimentCommand builds an UpsertExperimentConfigCommand for the fixture shed. Each call needs
// its own idempotency key/fingerprint unless the test deliberately reuses them.
func experimentCommand(key, fingerprint, itemLabel, kg string) domain.UpsertExperimentConfigCommand {
	return domain.UpsertExperimentConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-20",
			IdempotencyKey: key, RequestFingerprint: fingerprint,
		},
		ParkID: fcPark, ShedID: fcShed, FeedItemLabel: itemLabel, GramsPerHead: kg,
		ExperimentCategory: "control",
	}
}

// experimentShedStatuses returns every experiment_config row's status for the fixture shed, so a
// test can assert the WHOLE shed, not just the edited cell.
func experimentShedStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT feed_item_label, status
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid`, fcTenant, fcPark, fcShed)
	if err != nil {
		t.Fatalf("read experiment shed statuses: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var label, status string
		if err := rows.Scan(&label, &status); err != nil {
			t.Fatalf("scan experiment shed status: %v", err)
		}
		out[label] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate experiment shed statuses: %v", err)
	}
	return out
}

// TestUpsertExperimentConfigReactivatesWholeShedNotJustEditedCell is the P1 follow-up regression
// test: retiring a five-item shed then editing ONE old cell must bring the WHOLE shed back to
// 'active', not just the edited row. Before the fix, feeddirection's LoadConfigSnapshot (which
// loads only active rows and treats that set as the complete experiment list --
// ExperimentPlanner.PlanDaily) would see only 1 active row out of 5 and silently generate a
// truncated, underfeeding direction.
func TestUpsertExperimentConfigReactivatesWholeShedNotJustEditedCell(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	items := []string{"Concentrate", "Hybrid", "COFS", "Hedge Lucerne", "Dry Maize"}
	cells := make([]domain.ExperimentBatchCell, 0, len(items))
	for _, item := range items {
		cells = append(cells, domain.ExperimentBatchCell{FeedItemLabel: item, GramsPerHead: "1.500"})
	}
	enrollExperimentPen(t, ctx, repo, "key-retire5-insert", fcShed, "", "Arm A", cells)

	// Retire the whole shed. All five rows must flip to 'retired'.
	if _, err := repo.SetExperimentShedStatus(ctx, domain.SetExperimentShedStatusCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-20",
			IdempotencyKey: "key-retire5-status", RequestFingerprint: "fp-retire5-status",
		},
		ParkID: fcPark, ShedID: fcShed, Status: domain.ExperimentStatusRetired,
	}); err != nil {
		t.Fatalf("retire shed: %v", err)
	}
	statuses := experimentShedStatuses(t, ctx, pool)
	for _, item := range items {
		if statuses[item] != domain.ExperimentStatusRetired {
			t.Fatalf("after retire, %s status = %q, want retired", item, statuses[item])
		}
	}

	// Edit ONE old cell (a quantity correction, not a fresh insert -- the "lock the row" branch).
	editCmd := experimentCommand("key-retire5-edit", "fp-retire5-edit", "Concentrate", "2.000")
	if _, err := repo.UpsertExperimentConfig(ctx, editCmd); err != nil {
		t.Fatalf("edit one retired cell: %v", err)
	}

	// The ENTIRE shed must be active again, not just "Concentrate". A partial reactivation is
	// exactly the underfeed bug: ExperimentPlanner.Applies enrols the shed off ANY active row,
	// but LoadConfigSnapshot would load only the 1 reactivated row as the complete experiment
	// list, dropping the other 4 items from the generated direction.
	statuses = experimentShedStatuses(t, ctx, pool)
	for _, item := range items {
		if statuses[item] != domain.ExperimentStatusActive {
			t.Fatalf("after editing one cell, %s status = %q, want active (whole-shed reactivation)",
				item, statuses[item])
		}
	}
}

// TestUpsertShedFactorRejectsShedFromDifferentPark is the CR-03 regression: a caller supplying a
// park that exists and a shed that ALSO exists, but the shed belongs to a DIFFERENT park, must be
// rejected. Before this fix, UpsertShedFactor validated ParkID and ShedID as two independent
// requireLocation calls -- each proved existence + type, neither proved the shed's
// parent_location_id actually matched the supplied park -- so a shed-from-park-A write scoped
// under park-B would happily succeed and write a shed_factors row keyed by the WRONG park.
func TestUpsertShedFactorRejectsShedFromDifferentPark(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	cmd := domain.UpsertShedFactorCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-cr03-factor", RequestFingerprint: "fp-cr03-factor",
		},
		// fcPark is real; fcShedOtherPark is real but belongs to fcOtherPark, not fcPark.
		ParkID: fcPark, ShedID: fcShedOtherPark, FeedItemLabel: "Concentrate", Multiplier: "1.5000",
	}

	_, err := repo.UpsertShedFactor(ctx, cmd)
	if !errors.Is(err, ports.ErrShedNotFound) {
		t.Fatalf("UpsertShedFactor(park-A, shed-of-park-B) err = %v, want ErrShedNotFound", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_shed_factors
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`, fcTenant, fcShedOtherPark).Scan(&count); err != nil {
		t.Fatalf("count shed factor rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("shed factor rows for cross-park shed = %d, want 0 (no config/ledger rows created)", count)
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_config_write_log
WHERE tenant_id = $1::uuid AND idempotency_key = 'key-cr03-factor'`, fcTenant).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if ledgerCount != 0 {
		t.Fatalf("write-log ledger rows for rejected cross-park write = %d, want 0", ledgerCount)
	}
}

// TestUpsertExperimentConfigRejectsShedFromDifferentPark is the CR-03 regression for the experiment
// authoring path.
func TestUpsertExperimentConfigRejectsShedFromDifferentPark(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	cmd := domain.UpsertExperimentConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-cr03-exp", RequestFingerprint: "fp-cr03-exp",
		},
		ParkID: fcPark, ShedID: fcShedOtherPark, FeedItemLabel: "Concentrate", GramsPerHead: "2.000",
	}

	_, err := repo.UpsertExperimentConfig(ctx, cmd)
	if !errors.Is(err, ports.ErrShedNotFound) {
		t.Fatalf("UpsertExperimentConfig(park-A, shed-of-park-B) err = %v, want ErrShedNotFound", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`, fcTenant, fcShedOtherPark).Scan(&count); err != nil {
		t.Fatalf("count experiment config rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("experiment config rows for cross-park shed = %d, want 0", count)
	}
}

// TestSetExperimentShedStatusRejectsShedFromDifferentPark is the CR-03 regression for the
// whole-shed status-flip path.
func TestSetExperimentShedStatusRejectsShedFromDifferentPark(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	_, err := repo.SetExperimentShedStatus(ctx, domain.SetExperimentShedStatusCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-cr03-status", RequestFingerprint: "fp-cr03-status",
		},
		ParkID: fcPark, ShedID: fcShedOtherPark, Status: domain.ExperimentStatusRetired,
	})
	if !errors.Is(err, ports.ErrShedNotFound) {
		t.Fatalf("SetExperimentShedStatus(park-A, shed-of-park-B) err = %v, want ErrShedNotFound", err)
	}
}

// TestUpsertExperimentConfigSyncsShedMetadataAcrossCells is the CR-07 regression.
//
// head_count and experiment_category are shed-level facts even though feed_experiment_config
// stores one row per (shed, feed item). Before the fix, editing ONE cell updated only that row,
// so PlanDaily -- which reads cells[0].Category as the shed's category -- could keep serving a
// stale category/head-count (edit ignored) or an arbitrary sibling's value, depending on load
// order. The fix propagates every write's head_count/category to ALL of the shed's rows in the
// same transaction.
func TestUpsertExperimentConfigSyncsShedMetadataAcrossCells(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	items := []string{"Concentrate", "Fodder", "Mineral Mix"}
	cells := make([]domain.ExperimentBatchCell, 0, len(items))
	for _, item := range items {
		cells = append(cells, domain.ExperimentBatchCell{FeedItemLabel: item, GramsPerHead: "1.500"})
	}
	enrollExperimentPen(t, ctx, repo, "key-cr07-insert", fcShed, "", "control", cells)

	// Sanity: all three rows agree before the edit under test.
	before := experimentShedMetadata(t, ctx, pool)
	for _, item := range items {
		if before[item].category != "control" {
			t.Fatalf("before edit, %s arm = %q, want control", item, before[item].category)
		}
	}

	// Edit ONE cell's shed-level metadata: a new arm. (There is no head count to edit any more --
	// the pen's population is read live wherever it is needed.)
	editCmd := domain.UpsertExperimentConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-20",
			IdempotencyKey: "key-cr07-edit", RequestFingerprint: "fp-cr07-edit",
		},
		ParkID: fcPark, ShedID: fcShed, FeedItemLabel: "Concentrate", GramsPerHead: "1.750",
		ExperimentCategory: "treatment",
	}
	if _, err := repo.UpsertExperimentConfig(ctx, editCmd); err != nil {
		t.Fatalf("edit Concentrate cell: %v", err)
	}

	// EVERY row for the shed -- not just Concentrate -- must now report the new arm. A sibling still
	// showing "control" is the exact bug CR-07 describes: whichever row PlanDaily happens to load
	// first decides what the whole shed's direction prints.
	after := experimentShedMetadata(t, ctx, pool)
	for _, item := range items {
		if got := after[item]; got.category != "treatment" {
			t.Fatalf("after editing Concentrate, %s arm = %q, want treatment -- sibling row not synced", item, got.category)
		}
	}
	// The edited row's own quantity (grams_per_head) is per-item and must NOT have been overwritten
	// on the siblings: only the shed-level fields sync, not the per-cell quantity.
	var fodderKg string
	if err := pool.QueryRow(ctx, `
SELECT grams_per_head::text FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND feed_item_key = feed_config_norm('Fodder')`, fcTenant, fcPark, fcShed).Scan(&fodderKg); err != nil {
		t.Fatalf("read Fodder grams_per_head: %v", err)
	}
	if fodderKg != "1.500" {
		t.Fatalf("Fodder grams_per_head = %q, want unchanged 1.500 (only shed-level fields sync, not per-item quantity)", fodderKg)
	}
}

type experimentShedRow struct {
	category  string
	headCount int32
}

func experimentShedMetadata(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]experimentShedRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT feed_item_label, experiment_category, head_count
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid`, fcTenant, fcPark, fcShed)
	if err != nil {
		t.Fatalf("read experiment shed metadata: %v", err)
	}
	defer rows.Close()
	out := map[string]experimentShedRow{}
	for rows.Next() {
		var label, category string
		var headCount int32
		if err := rows.Scan(&label, &category, &headCount); err != nil {
			t.Fatalf("scan experiment shed metadata: %v", err)
		}
		out[label] = experimentShedRow{category: category, headCount: headCount}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate experiment shed metadata: %v", err)
	}
	return out
}

// ---------------------------------------------------------------------------
// Feed items (the catalog)
// ---------------------------------------------------------------------------

func feedItemCommand(key, fingerprint, label string) domain.CreateFeedItemCommand {
	return domain.CreateFeedItemCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: key, RequestFingerprint: fingerprint,
		},
		FeedItemLabel: label,
	}
}

// TestCreateFeedItemStoresUnmeasuredAttributesAsNull is the proof that only the database can give:
// an omitted attribute is a real SQL NULL in the stored row, not a 0.
//
// The fake-backed service test proves nil reaches the repository; this proves the repository binds
// it as NULL rather than letting a numeric column's default or a stray coalesce turn it into a
// measurement nobody took. The distinction is visible only in the row itself.
func TestCreateFeedItemStoresUnmeasuredAttributesAsNull(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	got, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00001", "fp-item", "RGS Concentrate"))
	if err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}
	if got.Outcome != domain.OutcomeInserted {
		t.Fatalf("outcome = %q, want %q — an add has no update branch", got.Outcome, domain.OutcomeInserted)
	}

	var label, status string
	var energy, dryMatter, wastage *string
	if err := pool.QueryRow(ctx, `
SELECT feed_item_label, energy_kcal_per_kg::text, dry_matter_factor::text, wastage_factor::text, status
FROM feed_item_catalog
WHERE feed_item_id = $1::uuid`, got.ResultRowID).
		Scan(&label, &energy, &dryMatter, &wastage, &status); err != nil {
		t.Fatalf("read stored feed item: %v", err)
	}
	if label != "RGS Concentrate" {
		t.Fatalf("feed_item_label = %q, want %q", label, "RGS Concentrate")
	}
	if status != "active" {
		t.Fatalf("status = %q, want active — an item added to the vocabulary is one the author intends to use", status)
	}
	for name, got := range map[string]*string{
		"energy_kcal_per_kg": energy,
		"dry_matter_factor":  dryMatter,
		"wastage_factor":     wastage,
	} {
		if got != nil {
			t.Fatalf("%s = %q, want SQL NULL — an unmeasured attribute must not be stored as a number", name, *got)
		}
	}
}

// TestCreateFeedItemAuthorsNoQuantity is the load-bearing test of this feature.
//
// Adding an item must leave feed_ration_rates, feed_shed_factors and feed_experiment_config
// UNTOUCHED. A convenience row seeded here would be a quantity nobody entered, and a seeded 0 would
// be worse than that: it would record "feed none of this item" for the combination it was created
// under, permanently and invisibly, which is exactly the configured-zero collapse the whole module
// is built to prevent.
func TestCreateFeedItemAuthorsNoQuantity(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00002", "fp-item", "Vijay Concentrate")); err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}
	// An EMPTY grid has no cells, so there is nothing to complete and no row is written. The item
	// authors no quantity of its own in any case — see the sibling below for the populated grid,
	// where cells are opened at 0 and 0 is not a quantity but the absence of one made authorable.
	for _, table := range []string{"feed_ration_rates", "feed_shed_factors", "feed_experiment_config"} {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE tenant_id = $1::uuid`, fcTenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s holds %d rows after adding a catalog item; adding an item must author no quantity", table, count)
		}
	}
}

// TestCreateFeedItemOpensZeroRowsInEveryExistingCell is the proof for the second half of the
// 2026-08-14 grid-completion rule, and the reason it exists is a real stranding: `Vijay Concentrate`
// and `RGS Concentrate` were added to the catalog on 2026-08-07 and never given a rate, which left
// them UNAUTHORABLE — the grid renders only rows that exist, so an item with no row has no cell to
// edit and no way to gain one.
//
// Three things are asserted together because each is a different way to get this wrong:
//
//  1. Every EXISTING cell gains an open row for the new item, at 0.
//  2. No cell is INVENTED. The authored grid is ragged on purpose, so filling the group x tag
//     cartesian would unblock feeding combinations nobody authored. Here the ragged shape is a
//     second group that exists in only one of the two tags.
//  3. Not one authored quantity moves. This is the assertion that would catch a fill written as an
//     upsert instead of an insert-where-absent.
func TestCreateFeedItemOpensZeroRowsInEveryExistingCell(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	// Three cells, deliberately ragged: Boer is authored in both tags, Sojat in only one. The
	// (Sojat, Pregnant) cell must NOT appear, even though both labels exist in the fixture.
	authored := []struct{ group, tag, grams string }{
		{"Boer", "Pregnant", "500.000"},
		{"Boer", "Non-Pregnant", "250.000"},
		{"Sojat", "Non-Pregnant", "125.000"},
	}
	for i, cell := range authored {
		cmd := rateCommand(fmt.Sprintf("key-cell-%02d", i), fmt.Sprintf("fp-cell-%02d", i), cell.grams, "2026-07-19")
		cmd.RationGroupLabel, cmd.ShedTagLabel = cell.group, cell.tag
		if _, err := repo.UpsertRationRate(ctx, cmd); err != nil {
			t.Fatalf("seed cell %s/%s: %v", cell.group, cell.tag, err)
		}
	}

	if _, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-fill", "fp-item-fill", "Vijay Concentrate")); err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}

	rows, err := pool.Query(ctx, `
SELECT ration_group_label, shed_tag_label, grams_per_head::text, source_system, valid_from::text
FROM feed_ration_rates
WHERE tenant_id = $1::uuid AND feed_item_key = feed_config_norm('Vijay Concentrate') AND valid_to IS NULL
ORDER BY ration_group_label, shed_tag_label`, fcTenant)
	if err != nil {
		t.Fatalf("read filled rows: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var group, tag, grams, source, validFrom string
		if err := rows.Scan(&group, &tag, &grams, &source, &validFrom); err != nil {
			t.Fatalf("scan filled row: %v", err)
		}
		if grams != "0.000" {
			t.Fatalf("%s/%s filled at %s; a completed cell carries 0, never a quantity nobody authored", group, tag, grams)
		}
		if source != "grid_fill" {
			t.Fatalf("%s/%s filled with source_system=%q; a machine-written row must stay distinguishable from an authored 'manual' zero", group, tag, source)
		}
		if validFrom != "2026-07-19" {
			t.Fatalf("%s/%s filled with valid_from=%q, want the command's Asia/Kolkata business date", group, tag, validFrom)
		}
		got = append(got, group+"/"+tag)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read filled rows: %v", err)
	}

	want := []string{"Boer/Non-Pregnant", "Boer/Pregnant", "Sojat/Non-Pregnant"}
	if !reflect.DeepEqual(got, want) {
		// A got of 4 with Sojat/Pregnant present means the fill walked the group x tag cartesian and
		// invented a cell; a got of 0 means it never ran.
		t.Fatalf("filled cells = %v, want %v", got, want)
	}

	// The seeded quantities are still exactly what was authored, each still on its 'manual' row.
	for _, cell := range authored {
		var grams, source string
		if err := pool.QueryRow(ctx, `
SELECT grams_per_head::text, source_system
FROM feed_ration_rates
WHERE tenant_id = $1::uuid
  AND ration_group_key = feed_config_norm($2) AND shed_tag_key = feed_config_norm($3)
  AND feed_item_key = feed_config_norm('Concentrate') AND valid_to IS NULL`,
			fcTenant, cell.group, cell.tag).Scan(&grams, &source); err != nil {
			t.Fatalf("read authored %s/%s: %v", cell.group, cell.tag, err)
		}
		if grams != cell.grams || source != "manual" {
			t.Fatalf("authored %s/%s is now %s g (%s); adding a feed item must never touch an authored quantity",
				cell.group, cell.tag, grams, source)
		}
	}
}

// TestRestoreFeedItemTopsUpCellsAddedWhileRetired covers the other door into the same defect.
//
// Retiring is a status flip, never a delete, so the item's own rates survive — but a cell authored
// while it was away has no row for it, and a cell with no row is BLOCKED rather than zero. Restoring
// an item into a state where some sheds cannot be fed would undo the guarantee the retire path
// documents.
func TestRestoreFeedItemTopsUpCellsAddedWhileRetired(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first := rateCommand("key-restore-00", "fp-restore-00", "500.000", "2026-07-19")
	if _, err := repo.UpsertRationRate(ctx, first); err != nil {
		t.Fatalf("seed first cell: %v", err)
	}
	created, err := repo.CreateFeedItem(ctx, feedItemCommand("key-restore-item", "fp-restore-item", "Vijay Concentrate"))
	if err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}

	retire := domain.SetFeedItemStatusCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-07-19",
			IdempotencyKey: "key-restore-off", RequestFingerprint: "fp-restore-off",
		},
		FeedItemID: created.ResultRowID, Status: domain.FeedItemStatusRetired,
	}
	if _, err := repo.SetFeedItemStatus(ctx, retire); err != nil {
		t.Fatalf("retire: %v", err)
	}

	// A cell authored while the item is retired. The item is out of the grid read and out of
	// generation, so nothing fills it now.
	second := rateCommand("key-restore-01", "fp-restore-01", "250.000", "2026-07-19")
	second.ShedTagLabel = "Non-Pregnant"
	if _, err := repo.UpsertRationRate(ctx, second); err != nil {
		t.Fatalf("seed second cell: %v", err)
	}

	restore := retire
	restore.Status = domain.FeedItemStatusActive
	restore.IdempotencyKey, restore.RequestFingerprint = "key-restore-on", "fp-restore-on"
	if _, err := repo.SetFeedItemStatus(ctx, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var cells int
	var minValidFrom, maxValidFrom string
	if err := pool.QueryRow(ctx, `
SELECT count(*), min(valid_from)::text, max(valid_from)::text FROM feed_ration_rates
WHERE tenant_id = $1::uuid AND feed_item_key = feed_config_norm('Vijay Concentrate')
  AND grams_per_head = 0 AND valid_to IS NULL`, fcTenant).Scan(&cells, &minValidFrom, &maxValidFrom); err != nil {
		t.Fatalf("count restored cells: %v", err)
	}
	if cells != 2 {
		t.Fatalf("restored item covers %d cells, want 2; a cell authored during retirement is BLOCKED, not zero", cells)
	}
	if minValidFrom != "2026-07-19" || maxValidFrom != "2026-07-19" {
		t.Fatalf("restored cells valid_from range = %s..%s, want restore command business date", minValidFrom, maxValidFrom)
	}
}

// TestCreateFeedItemDuplicateLabelIsRejectedOnTheNormalizedKey proves the duplicate check runs on
// feed_config_norm, the same expression the stored key column is generated from — not on the raw
// text.
//
// "Dry Masoor Bhusa" and "  dry masoor bhusa " are ONE item as far as every rate is concerned. A
// raw-text comparison would let the second one through, and the catalog would then show two entries
// that both resolve to the same key while every rate keyed on that label points at the first.
func TestCreateFeedItemDuplicateLabelIsRejectedOnTheNormalizedKey(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00003", "fp-item", "Dry Masoor Bhusa")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	// A DIFFERENT idempotency key, so this is a genuinely new request rather than a replay.
	_, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00004", "fp-item-2", "  dry masoor bhusa "))
	if !errors.Is(err, ports.ErrFeedItemExists) {
		t.Fatalf("duplicate add error = %v, want ErrFeedItemExists", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM feed_item_catalog WHERE tenant_id = $1::uuid`, fcTenant).Scan(&count); err != nil {
		t.Fatalf("count catalog: %v", err)
	}
	if count != 1 {
		t.Fatalf("catalog holds %d rows, want 1 — the rejected duplicate must not have been written", count)
	}
}

// TestCreateFeedItemAppendsToTheEndOfTheCatalog proves an absent display_order resolves to the
// catalog's maximum + 1 rather than to the column's DEFAULT 0.
//
// Taking the default would place every newly added item FIRST in every feed-item dropdown on the
// screen, ahead of the items the farm actually uses daily — a silent reordering of the whole
// vocabulary as a side effect of adding one name.
func TestCreateFeedItemAppendsToTheEndOfTheCatalog(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1::uuid, 'Existing Item', 7, 'active')`, fcTenant); err != nil {
		t.Fatalf("seed existing catalog row: %v", err)
	}

	got, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00005", "fp-item", "Appended Item"))
	if err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}
	var order int32
	if err := pool.QueryRow(ctx,
		`SELECT display_order FROM feed_item_catalog WHERE feed_item_id = $1::uuid`, got.ResultRowID).Scan(&order); err != nil {
		t.Fatalf("read display_order: %v", err)
	}
	if order != 8 {
		t.Fatalf("display_order = %d, want 8 (max 7 + 1) — an absent order must append, not take the column default of 0", order)
	}
}

// TestCreateFeedItemExactReplayReturnsOriginalWithoutASecondRow proves the add carries the same
// idempotency contract as every other write here.
//
// It matters more than usual on a create: without it a browser retry would insert nothing (the
// unique index holds) but would answer "already exists" for a name the operator submitted once,
// which reads as a failure of their own action.
func TestCreateFeedItemExactReplayReturnsOriginalWithoutASecondRow(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00006", "fp-item", "Replayed Item"))
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	replay, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00006", "fp-item", "Replayed Item"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Replayed {
		t.Fatalf("replay reported idempotent_replay=false")
	}
	if replay.ResultRowID != first.ResultRowID {
		t.Fatalf("replay row = %q, want the original %q", replay.ResultRowID, first.ResultRowID)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM feed_item_catalog WHERE tenant_id = $1::uuid`, fcTenant).Scan(&count); err != nil {
		t.Fatalf("count catalog: %v", err)
	}
	if count != 1 {
		t.Fatalf("catalog holds %d rows after a replay, want 1", count)
	}
}

// TestCreateFeedItemIsRecordedInTheWriteLog proves migration 000136 actually widened the ledger's
// write_kind vocabulary.
//
// This is not a formality. The CHECK is closed and the ledger row shares the insert's transaction,
// so before the migration this write would have failed the ledger insert and rolled the whole add
// back — the endpoint would 500 on every attempt. The test therefore fails loudly if the migration
// is ever reverted without the code.
func TestCreateFeedItemIsRecordedInTheWriteLog(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)

	got, err := repo.CreateFeedItem(ctx, feedItemCommand("key-item-00007", "fp-item", "Ledgered Item"))
	if err != nil {
		t.Fatalf("CreateFeedItem: %v", err)
	}
	var kind, outcome, actor, effectiveFrom, resultRow string
	if err := pool.QueryRow(ctx, `
SELECT write_kind, outcome, actor_ref, effective_from::text, result_row_id::text
FROM feed_config_write_log
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, fcTenant, "key-item-00007").
		Scan(&kind, &outcome, &actor, &effectiveFrom, &resultRow); err != nil {
		t.Fatalf("read write log: %v", err)
	}
	if kind != domain.WriteKindFeedItem {
		t.Fatalf("write_kind = %q, want %q", kind, domain.WriteKindFeedItem)
	}
	if outcome != domain.OutcomeInserted || resultRow != got.ResultRowID {
		t.Fatalf("ledger row = (%s, %s), want (inserted, %s)", outcome, resultRow, got.ResultRowID)
	}
	// The catalog is not effective-dated, but the ledger still records WHEN the vocabulary changed.
	if actor != "tester" || effectiveFrom != "2026-07-19" {
		t.Fatalf("ledger audit = (%s, %s), want (tester, 2026-07-19)", actor, effectiveFrom)
	}
}

// =================================================================================================
// Session slots — the write that decides WHETHER a feed is served.
//
// These run against the real schema because every risk here is in SQL: the predicate that decides
// "already declared", the coverage gate that reads the whole grid, and the slot-number derivation
// that has to satisfy a partial unique index. A fake cannot see any of them.
// =================================================================================================

// seedSession gives the park a feeding session. Slots are FK'd to it, so a slot test without one
// would only ever prove the fail-closed path.
func seedSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionNo int, label, split string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, display_order, status)
VALUES ($1::uuid, $2::uuid, $3, $4, $5::numeric, $3, 'active')
ON CONFLICT DO NOTHING`, fcTenant, fcPark, sessionNo, label, split); err != nil {
		t.Fatalf("seed session %d: %v", sessionNo, err)
	}
}

func sessionSlotCommand(sessionNo int32, feedItem string, declared bool, key, effectiveFrom string) domain.SetSessionTemplateItemCommand {
	return domain.SetSessionTemplateItemCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: effectiveFrom,
			IdempotencyKey: key, RequestFingerprint: "fp-" + key,
		},
		ParkID: fcPark, SessionNo: sessionNo, FeedItemLabel: feedItem, Declared: declared,
	}
}

// declaredFeeds reads the session's recipe under the predicate GENERATION uses, which is the only
// predicate that answers "is this feed actually being served".
func declaredFeeds(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionNo int32, asOfDate string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT feed_item_label FROM feed_session_template_items
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND session_no = $3
  AND status = 'active' AND valid_from <= $4::date AND (valid_to IS NULL OR valid_to > $4::date)
ORDER BY slot_no`, fcTenant, fcPark, sessionNo, asOfDate)
	if err != nil {
		t.Fatalf("read declared feeds: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan declared feed: %v", err)
		}
		out = append(out, label)
	}
	return out
}

// TestDeclareSessionFeedRefusesAFeedTheParkCannotPriceEverywhere is the gate, and it is the reason
// the grid had to be completed before this write could exist.
//
// A declared slot is priced for EVERY shed in the park. A cell with no open rate is BLOCKED rather
// than zero — the sheet fails outright with `no currently-open ration rate for ...` — so declaring a
// feed that is unpriced in even one cell would take those sheds down at the next issue, hours after
// the author pressed a button that appeared to work.
func TestDeclareSessionFeedRefusesAFeedTheParkCannotPriceEverywhere(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)

	// Two cells, and Sirohi Concentrate is priced in only one of them.
	for i, cell := range []struct{ group, tag string }{{"Boer", "Pregnant"}, {"Boer", "Non-Pregnant"}} {
		cmd := rateCommand(fmt.Sprintf("key-slotcell-%02d", i), fmt.Sprintf("fp-slotcell-%02d", i), "500.000", "2026-07-19")
		cmd.RationGroupLabel, cmd.ShedTagLabel = cell.group, cell.tag
		if _, err := repo.UpsertRationRate(ctx, cmd); err != nil {
			t.Fatalf("seed cell: %v", err)
		}
	}
	// Priced in ONE cell only — the partial state the gate exists for.
	partial := rateCommand("key-partial", "fp-partial", "100.000", "2026-07-19")
	partial.FeedItemLabel = "Hybrid"
	if _, err := repo.UpsertRationRate(ctx, partial); err != nil {
		t.Fatalf("seed partial rate: %v", err)
	}

	_, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Hybrid", true, "key-slot-refuse", "2026-07-19"))
	if !errors.Is(err, ports.ErrSlotRatesIncomplete) {
		t.Fatalf("err = %v, want ErrSlotRatesIncomplete", err)
	}
	if got := declaredFeeds(t, ctx, pool, 1, "2026-07-19"); len(got) != 0 {
		t.Fatalf("a REFUSED declare still wrote a slot: %v", got)
	}

	// Fully priced, so it is accepted — proving the refusal above is the coverage gate and not a
	// blanket rejection.
	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-slot-ok", "2026-07-19")); err != nil {
		t.Fatalf("declare fully-priced feed: %v", err)
	}
	if got := declaredFeeds(t, ctx, pool, 1, "2026-07-19"); len(got) != 1 || got[0] != "Concentrate" {
		t.Fatalf("declared feeds = %v, want [Concentrate]", got)
	}
}

func seedCorrectionBreedsForSlots(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status)
VALUES ($1::uuid, 'Concentrate', 'active'), ($1::uuid, 'Hybrid', 'active')
ON CONFLICT DO NOTHING`, fcTenant); err != nil {
		t.Fatalf("seed feed items: %v", err)
	}
}

// TestWithdrawSessionFeedClosesTheRowAndNeverDeletesIt pins the non-destructive half: a feed taken
// off the recipe stops being served, but the row survives so sheets already issued from it stay
// explainable.
func TestWithdrawSessionFeedClosesTheRowAndNeverDeletesIt(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-w-cell", "fp-w-cell", "500.000", "2026-07-19")); err != nil {
		t.Fatalf("seed cell: %v", err)
	}
	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-w-add", "2026-07-19")); err != nil {
		t.Fatalf("declare: %v", err)
	}

	// A LATER business date, so the ordinary window-closing branch runs rather than the same-day one.
	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", false, "key-w-del", "2026-07-20")); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	// The withdrawal closes the window AT 2026-07-20, so the feed stops being served from that
	// date -- and remains served on 2026-07-19, which is the whole point of closing the row
	// instead of deleting it: sheets already issued for the 19th stay explainable.
	if got := declaredFeeds(t, ctx, pool, 1, "2026-07-20"); len(got) != 0 {
		t.Fatalf("withdrawn feed is still served on the withdrawal date: %v", got)
	}
	if got := declaredFeeds(t, ctx, pool, 1, "2026-07-19"); len(got) != 1 || got[0] != "Concentrate" {
		t.Fatalf("the day before the withdrawal no longer reports what was served: %v", got)
	}

	var stored int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_session_template_items
WHERE tenant_id = $1::uuid AND feed_item_key = feed_config_norm('Concentrate') AND valid_to = '2026-07-20'`,
		fcTenant).Scan(&stored); err != nil {
		t.Fatalf("count closed rows: %v", err)
	}
	if stored != 1 {
		t.Fatalf("%d closed rows, want 1 — a withdrawal must CLOSE the row, never delete it", stored)
	}

	// Withdrawing again is reported, not silently accepted: the author may be on the wrong session.
	_, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", false, "key-w-del2", "2026-07-21"))
	if !errors.Is(err, ports.ErrSlotNotDeclared) {
		t.Fatalf("err = %v, want ErrSlotNotDeclared", err)
	}
}

// TestSessionFeedsAreIndependentPerSession is the parity proof for the split gotcha: a feed added to
// the morning session is NOT served in the evening. The two sessions each serve their own share of
// the daily grid quantity, so declaring in one is a real and partial decision.
func TestSessionFeedsAreIndependentPerSession(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedSession(t, ctx, pool, 2, "Evening", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-s-cell", "fp-s-cell", "500.000", "2026-07-19")); err != nil {
		t.Fatalf("seed cell: %v", err)
	}

	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-s-1", "2026-07-19")); err != nil {
		t.Fatalf("declare morning: %v", err)
	}
	if got := declaredFeeds(t, ctx, pool, 2, "2026-07-19"); len(got) != 0 {
		t.Fatalf("evening session serves %v after declaring only on the morning session", got)
	}

	// The same feed on the OTHER session is a separate slot, not a conflict.
	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(2, "Concentrate", true, "key-s-2", "2026-07-19")); err != nil {
		t.Fatalf("declare evening: %v", err)
	}
	if got := declaredFeeds(t, ctx, pool, 2, "2026-07-19"); len(got) != 1 || got[0] != "Concentrate" {
		t.Fatalf("evening feeds = %v, want [Concentrate]", got)
	}

	// An unknown session is refused rather than FK-violating.
	_, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(9, "Concentrate", true, "key-s-9", "2026-07-19"))
	if !errors.Is(err, ports.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

// TestListSessionTemplatesReadsRecipeAsOfExplicitBusinessDate proves the config UI read uses the
// same explicit business date as generation. A SQL CURRENT_DATE predicate would make this depend on
// the database session timezone and hide the slot around the IST/UTC boundary.
func TestListSessionTemplatesReadsRecipeAsOfExplicitBusinessDate(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-asof-cell", "fp-asof-cell", "500.000", "2026-07-19")); err != nil {
		t.Fatalf("seed cell: %v", err)
	}
	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-asof-add", "2026-07-20")); err != nil {
		t.Fatalf("declare future feed: %v", err)
	}

	before, err := repo.ListSessionTemplates(ctx, domain.SessionTemplateQuery{
		TenantID: fcTenant, ParkID: fcPark, AsOfDate: "2026-07-19", Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("list before as-of: %v", err)
	}
	if len(before.Items) != 1 || len(before.Items[0].Items) != 0 {
		t.Fatalf("before date items = %#v, want no served feeds", before.Items)
	}

	onDate, err := repo.ListSessionTemplates(ctx, domain.SessionTemplateQuery{
		TenantID: fcTenant, ParkID: fcPark, AsOfDate: "2026-07-20", Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("list on as-of: %v", err)
	}
	if len(onDate.Items) != 1 || len(onDate.Items[0].Items) != 1 || onDate.Items[0].Items[0].FeedItemLabel != "Concentrate" {
		t.Fatalf("on-date items = %#v, want Concentrate served", onDate.Items)
	}
}

// TestDeclareSessionFeedIsIdempotentAndAppendsInPackingOrder covers the replay contract and the slot
// numbering, which has to satisfy a partial unique index that counts WITHDRAWN rows too.
func TestDeclareSessionFeedIsIdempotentAndAppendsInPackingOrder(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-i-cell", "fp-i-cell", "500.000", "2026-07-19")); err != nil {
		t.Fatalf("seed cell: %v", err)
	}
	hybrid := rateCommand("key-i-cell2", "fp-i-cell2", "100.000", "2026-07-19")
	hybrid.FeedItemLabel = "Hybrid"
	if _, err := repo.UpsertRationRate(ctx, hybrid); err != nil {
		t.Fatalf("seed hybrid cell: %v", err)
	}

	first, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-i-1", "2026-07-19"))
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if first.Outcome != domain.OutcomeInserted {
		t.Fatalf("outcome = %q, want inserted", first.Outcome)
	}

	// A DIFFERENT key, same intent. Not an idempotent replay — a genuine second request that finds
	// the state already correct, which must not open a second window or churn the packing order.
	again, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-i-2", "2026-07-19"))
	if err != nil {
		t.Fatalf("re-declare: %v", err)
	}
	if again.Outcome != domain.OutcomeUnchanged {
		t.Fatalf("outcome = %q, want unchanged", again.Outcome)
	}

	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Hybrid", true, "key-i-3", "2026-07-19")); err != nil {
		t.Fatalf("declare second feed: %v", err)
	}
	// Appended, never inserted mid-list: slot order is the PACKING order and renumbering it would
	// reorder what packers already know.
	if got := declaredFeeds(t, ctx, pool, 1, "2026-07-19"); len(got) != 2 || got[0] != "Concentrate" || got[1] != "Hybrid" {
		t.Fatalf("declared feeds = %v, want [Concentrate Hybrid] in packing order", got)
	}
}

// TestDeclareSessionFeedRefusesEarlierEditAgainstFutureRecipe catches the easy false-green in the
// declare path: the row lock must see future-dated active rows, but an earlier declare must not
// report "unchanged" because generation will not serve that row on the earlier business date.
func TestDeclareSessionFeedRefusesEarlierEditAgainstFutureRecipe(t *testing.T) {
	ctx := context.Background()
	pool := setupFeedConfigDB(t, ctx)
	repo := fcRepo(pool)
	seedSession(t, ctx, pool, 1, "Morning", "0.5")
	seedCorrectionBreedsForSlots(t, ctx, pool)
	if _, err := repo.UpsertRationRate(ctx, rateCommand("key-future-cell", "fp-future-cell", "500.000", "2026-07-19")); err != nil {
		t.Fatalf("seed cell: %v", err)
	}

	if _, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-future-add", "2026-07-22")); err != nil {
		t.Fatalf("declare future feed: %v", err)
	}
	_, err := repo.SetSessionTemplateItem(ctx, sessionSlotCommand(1, "Concentrate", true, "key-future-earlier", "2026-07-20"))
	if !errors.Is(err, ports.ErrFutureDatedRow) {
		t.Fatalf("err = %v, want ErrFutureDatedRow", err)
	}
}

// fcSeedCatalog registers feed items as ACTIVE. The rate listing deliberately scopes itself to
// catalogued, active items -- the authoring screen and the feed sheet must not disagree about what
// is fed -- so a fixture that writes rates without cataloguing their item lists nothing at all.
func fcSeedCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, status)
VALUES ($1::uuid, $2, 'active')
ON CONFLICT DO NOTHING`, fcTenant, label); err != nil {
			t.Fatalf("seed feed item %s: %v", label, err)
		}
	}
}
