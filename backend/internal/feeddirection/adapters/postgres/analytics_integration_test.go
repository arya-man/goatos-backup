package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Grain proofs for the Feed Analytics rollup, seeded through PersistIssue (the
// production write path) — never by hand-inserting rollup rows.
//
// The adversarial fixture is the one the aggregate rules demand: one pen-grain
// fed in TWO sessions (head-days must not double), a shed split into TWO
// partitions (two grains, heads add), a BLOCKED cell (contributes nothing), an
// AUTHORED ZERO (contributes 0 kg but keeps its heads), TWO dates, and an
// EXPERIMENT issue that must be invisible to the directed rollup.

func analyticsCells() []domain.StoredCell {
	cell := func(shed, partition, breed string, session int32, heads int64, item, key string, qty *string, rowSeq, itemSeq int32) domain.StoredCell {
		c := domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: "Castro",
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: breed,
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: heads, Workflow: domain.WorkflowNormal,
			FeedItemLabel: item, FeedItemKey: key, QuantityKg: qty,
			SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: itemSeq,
		}
		if qty == nil {
			code := domain.BlockReasonNoRationRate
			detail := "no rate"
			c.BlockedReasonCode, c.BlockedReasonDetail = &code, &detail
		}
		return c
	}
	return []domain.StoredCell{
		// Shed A partition 1: 10 heads, both sessions carry Concentrate 1.0 kg each.
		// Head-days for the grain must read 10, kg must read 2.0.
		cell(fdiShedA, "1", "Beetal", 1, 10, "Concentrate", "concentrate", kg("1.000"), 0, 0),
		cell(fdiShedA, "1", "Beetal", 2, 10, "Concentrate", "concentrate", kg("1.000"), 1, 0),
		// Shed A partition 2: a DIFFERENT grain of the same shed — 5 more heads.
		cell(fdiShedA, "2", "Beetal", 1, 5, "Concentrate", "concentrate", kg("0.500"), 2, 0),
		// Shed A partition 1 also gets Hay: BLOCKED — no kg, and its presence must
		// not disturb the concentrate figures.
		cell(fdiShedA, "1", "Beetal", 1, 10, "Hay", "hay", nil, 0, 1),
		// Shed B: authored ZERO of Milk — 0 kg is a real instruction; 8 heads count.
		cell(fdiShedB, "", "Sojat", 1, 8, "Milk", "milk", kg("0.000"), 3, 0),
	}
}

func TestDirectedAnalyticsGrainProofs(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	persist := func(feedDay, workflow, fingerprint string, cells []domain.StoredCell) {
		t.Helper()
		cmd := ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: workflow,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":" + workflow,
			GeneratedBy:    "test", Cells: cells,
		}
		if _, err := repo.PersistIssue(ctx, cmd); err != nil {
			t.Fatalf("persist %s/%s: %v", feedDay, workflow, err)
		}
	}

	persist("2026-07-30", domain.WorkflowNormal, "fp-day1", analyticsCells())
	// Day 2: same sheet again, so the series has two points.
	persist("2026-07-31", domain.WorkflowNormal, "fp-day2", analyticsCells())
	// An experiment issue the SAME day — absolute kg, informational heads. The
	// directed rollup must not see one gram of it.
	exp := []domain.StoredCell{{
		ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
		PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
		ExperimentArm: "Mesha TMR", SessionNo: 1, SessionLabel: "S",
		HeadCount: 999, HeadCountInformational: true, Workflow: domain.WorkflowExperiment,
		FeedItemLabel: "Mesha TMR", FeedItemKey: "mesha tmr", QuantityKg: kg("500.000"),
		SessionTotalKg: "500.000",
	}}
	persist("2026-07-30", domain.WorkflowExperiment, "fp-exp", exp)

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("DirectedAnalytics: %v", err)
	}

	// ---- Day totals: two days, identical figures. ----
	if len(got.Days) != 2 {
		t.Fatalf("want 2 day totals, got %d: %+v", len(got.Days), got.Days)
	}
	day := got.Days[0]
	if day.FeedDay != "2026-07-30" {
		t.Fatalf("day order: %+v", got.Days)
	}
	// kg = 1.0 + 1.0 + 0.5 + 0 (authored zero) and NOTHING from the blocked cell
	// or the 500 kg experiment sheet.
	if day.DirectedKg != "2.500" {
		t.Errorf("day kg: want 2.500 (blocked adds nothing, experiment invisible), got %q", day.DirectedKg)
	}
	// Heads: partition1 (10, counted ONCE across sessions and items) +
	// partition2 (5) + shed B (8) — never the experiment's 999.
	if day.HeadDays != 23 {
		t.Errorf("day head-days: want 23 (pen-grain once, partitions apart), got %d", day.HeadDays)
	}
	// 2.5 kg × 1000 / 23 heads = 108.7 g.
	if day.PerHeadGrams != "108.7" {
		t.Errorf("day per-head: want 108.7, got %q", day.PerHeadGrams)
	}

	// ---- Per-item series for day 1. ----
	items := map[string]domain.DirectedDayItem{}
	for _, it := range got.Items {
		if it.FeedDay == "2026-07-30" {
			items[it.FeedItemKey] = it
		}
	}
	conc, ok := items["concentrate"]
	if !ok {
		t.Fatalf("concentrate series missing: %+v", got.Items)
	}
	if conc.DirectedKg != "2.500" || conc.HeadDays != 15 {
		t.Errorf("concentrate: want 2.500 kg over 15 heads (two sessions of pen 1 are ONE grain; pen 2 adds 5), got %q kg over %d", conc.DirectedKg, conc.HeadDays)
	}
	// 2.5 kg × 1000 / 15 = 166.7 g per head — divided by the heads fed THIS item.
	if conc.PerHeadGrams != "166.7" {
		t.Errorf("concentrate per-head: want 166.7, got %q", conc.PerHeadGrams)
	}
	milk, ok := items["milk"]
	if !ok {
		t.Fatalf("authored-zero milk series missing — a 0 kg instruction is real: %+v", got.Items)
	}
	if milk.DirectedKg != "0.000" || milk.HeadDays != 8 || milk.PerHeadGrams != "0.0" {
		t.Errorf("milk: want 0.000 kg, 8 heads, 0.0 g, got %+v", milk)
	}
	hay, ok := items["hay"]
	if !ok {
		t.Fatalf("blocked hay grain missing from the item series: %+v", got.Items)
	}
	if hay.DirectedKg != "0" || hay.PerHeadGrams != "" {
		t.Errorf("hay (every cell blocked): want kg \"0\" and per-head \"\", got %+v", hay)
	}
	if _, leaked := items["mesha tmr"]; leaked {
		t.Errorf("experiment feed item leaked into the directed rollup")
	}

	// ---- Park filter: a park the fixture never fed returns empty, not zeros. ----
	other := uuid.New()
	filtered, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		ParkIDs: []uuid.UUID{other}, DateFrom: window.DateFrom, DateTo: window.DateTo,
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(filtered.Days) != 0 || len(filtered.Items) != 0 {
		t.Errorf("foreign park filter: want empty, got %+v", filtered)
	}
}

// Execution + experiment rollups on the real schema. Completion/task rows are
// inserted directly at their own natural grain — this is a package read-model
// test of the status counting, not an E2E of the proof write paths (those have
// their own verification-gate integration tests).
func TestExecutionAndExperimentAnalytics(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	// Transport FKs onto locations: register the shed.
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($2::uuid, $1::uuid, 'shed', 'CASTRO', 'Castro', 'active', $3::uuid)
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdiShedA, fdiPark)

	verifiedAt := time.Date(2026, 7, 30, 11, 0, 0, 0, biztime.DefaultLocation())
	createdAt := verifiedAt.Add(-90 * time.Minute)
	// Packing: one verified (90 min latency), one awaiting, one rework.
	exec(`INSERT INTO feed_packing_completions
  (tenant_id, park_id, shed_id, session_no, target_date, workflow, status, packing_proof_ref, verified_at, created_at, idempotency_key)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-30', 'normal', 'completed', 'proof-1', $4, $5, 'k1'),
  ($1::uuid, $2::uuid, $3::uuid, 2, DATE '2026-07-30', 'normal', 'pending_verification', 'proof-2', NULL, $5, 'k2'),
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-31', 'normal', 'rework', NULL, NULL, $5, 'k3')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt, createdAt)
	// Distribution: one verified (30 min latency) — daily median over 90 and 30 is 60.
	exec(`INSERT INTO feed_distribution_completions
  (tenant_id, park_id, shed_id, session_no, target_date, workflow, status, distribution_proof_ref, water_proof_ref, feed_weight_proof_ref, verified_at, created_at, idempotency_key)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-30', 'normal', 'completed', 'd-1', 'w-1', 'f-1', $4, $5, 'k4')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt, verifiedAt.Add(-30*time.Minute))
	// Transport: one completed, one still due.
	exec(`INSERT INTO feed_transport_tasks (tenant_id, park_id, shed_id, business_date, scheduled_at, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, DATE '2026-07-30', $4, 'completed'),
       ($1::uuid, $2::uuid, $3::uuid, DATE '2026-07-31', $4, 'due')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt)

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.ExecutionAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("ExecutionAnalytics: %v", err)
	}
	if len(got.Days) != 2 {
		t.Fatalf("want 2 execution days, got %+v", got.Days)
	}
	d1, d2 := got.Days[0], got.Days[1]
	if d1.Date != "2026-07-30" || d1.PackingVerified != 1 || d1.PackingAwaiting != 1 || d1.DistributionVerified != 1 || d1.TransportCompleted != 1 {
		t.Errorf("day1 counts wrong: %+v", d1)
	}
	if d1.MedianVerifyLatencyMinutes == nil || *d1.MedianVerifyLatencyMinutes != 60 {
		t.Errorf("day1 median latency: want 60 (median of 90 and 30), got %v", d1.MedianVerifyLatencyMinutes)
	}
	if d2.Date != "2026-07-31" || d2.PackingRework != 1 || d2.TransportOpen != 1 || d2.MedianVerifyLatencyMinutes != nil {
		t.Errorf("day2 counts wrong: %+v", d2)
	}

	// Experiment series through the production write path: two arms, one with
	// two pens of one shed — pens count by pen-grain, kg is the authored total.
	expCell := func(partition, arm, qty string, rowSeq int32) domain.StoredCell {
		return domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: "Beetal",
			ExperimentArm: arm, SessionNo: 1, SessionLabel: "S",
			HeadCount: 7, HeadCountInformational: true, Workflow: domain.WorkflowExperiment,
			FeedItemLabel: "Mesha TMR", FeedItemKey: "mesha tmr", QuantityKg: kg(qty),
			SessionTotalKg: qty, RowSeq: rowSeq,
		}
	}
	cmd := ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowExperiment,
		IssuedAt: verifiedAt, Fingerprint: "fp-exp-arms",
		IdempotencyKey: "issue:exp:" + fdiTenant, GeneratedBy: "test",
		Cells: []domain.StoredCell{
			expCell("1", "Mesha TMR — adult", "40.000", 0),
			expCell("2", "Mesha TMR — adult", "35.000", 1),
			expCell("3", "Sorghum pellet mix", "20.000", 2),
		},
	}
	if _, err := repo.PersistIssue(ctx, cmd); err != nil {
		t.Fatalf("persist experiment: %v", err)
	}
	arms, err := repo.ExperimentAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("ExperimentAnalytics: %v", err)
	}
	if len(arms.Arms) != 2 {
		t.Fatalf("want 2 arms, got %+v", arms.Arms)
	}
	adult := arms.Arms[0]
	if adult.ExperimentArm != "Mesha TMR — adult" || adult.AbsoluteKg != "75.000" || adult.Pens != 2 {
		t.Errorf("adult arm: want 75.000 kg over 2 pens, got %+v", adult)
	}
	if arms.Arms[1].AbsoluteKg != "20.000" || arms.Arms[1].Pens != 1 {
		t.Errorf("sorghum arm: %+v", arms.Arms[1])
	}
}
