package postgres

import (
	"context"
	"fmt"
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

func TestDirectedAnalyticsOneToManyGrainProofs(t *testing.T) {
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
	// An experiment issue the SAME day. Since 2026-08-19 (maintainer decision)
	// the rollup counts BOTH workflows: the whole farm eats, so experiment kg
	// and heads join the series. This extends the OneToMany cardinality proof:
	// the pen key deliberately COLLIDES with the normal sheet's shed A
	// partition 1 grain to prove the workflow column keeps the two pens apart
	// instead of MAX()-collapsing them.
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

	// ---- Day totals: two days. Day 1 carries the experiment sheet too. ----
	if len(got.Days) != 2 {
		t.Fatalf("want 2 day totals, got %d: %+v", len(got.Days), got.Days)
	}
	day := got.Days[0]
	if day.FeedDay != "2026-07-30" {
		t.Fatalf("day order: %+v", got.Days)
	}
	// kg = 1.0 + 1.0 + 0.5 + 0 (authored zero) + 500 experiment; the blocked
	// cell still adds nothing.
	if day.DirectedKg != "502.500" {
		t.Errorf("day kg: want 502.500 (normal 2.5 + experiment 500, blocked adds nothing), got %q", day.DirectedKg)
	}
	// Heads: partition1 (10, counted ONCE across sessions and items) +
	// partition2 (5) + shed B (8) + the experiment pen (999) — the colliding
	// pen key stays a separate grain because workflow is part of the grain.
	if day.HeadDays != 1022 {
		t.Errorf("day head-days: want 1022 (23 normal + 999 experiment, workflows apart), got %d", day.HeadDays)
	}
	// 502.5 kg × 1000 / 1022 heads = 491.7 g.
	if day.PerHeadGrams != "491.7" {
		t.Errorf("day per-head: want 491.7, got %q", day.PerHeadGrams)
	}
	// Day 2 has no experiment issue and keeps the normal-only figures.
	day2 := got.Days[1]
	if day2.DirectedKg != "2.500" || day2.HeadDays != 23 {
		t.Errorf("day 2: want 2.500 kg over 23 heads (no experiment issue that day), got %q over %d", day2.DirectedKg, day2.HeadDays)
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
	tmr, ok := items["mesha_tmr"] // the write path normalizes the fixture's "mesha tmr"
	if !ok {
		t.Fatalf("experiment feed item missing from the directed rollup (2026-08-19: both workflows count): %+v", got.Items)
	}
	if tmr.DirectedKg != "500.000" || tmr.HeadDays != 999 {
		t.Errorf("experiment item: want 500.000 kg over 999 heads, got %q over %d", tmr.DirectedKg, tmr.HeadDays)
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
func TestExecutionAnalyticsStatusMatrixAndExperimentArms(t *testing.T) {
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

	// Experiment series through the production write path (same StatusMatrix
	// fixture, states issued/amended/locked all counted). Since 2026-08-19
	// (maintainer decision) the payload is SHED-WISE: one row per (day, pen)
	// labelled by the oploc display, plus a per-(day, feed item) series; the
	// arm rides each pen row and the per-arm pen count is gone.
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
	exp, err := repo.ExperimentAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("ExperimentAnalytics: %v", err)
	}
	if len(exp.Sheds) != 3 {
		t.Fatalf("want 3 pen rows, got %+v", exp.Sheds)
	}
	// Ordered by shed label then partition key: Castro 1, 2, 3 — each labelled
	// by the canonical oploc display (numeric pens join with a space).
	pen1 := exp.Sheds[0]
	if pen1.LocationDisplay != "Castro 1" || pen1.ExperimentArm != "Mesha TMR — adult" || pen1.AbsoluteKg != "40.000" {
		t.Errorf("pen 1: want Castro 1 / Mesha TMR — adult / 40.000, got %+v", pen1)
	}
	if exp.Sheds[1].LocationDisplay != "Castro 2" || exp.Sheds[1].AbsoluteKg != "35.000" {
		t.Errorf("pen 2: %+v", exp.Sheds[1])
	}
	if exp.Sheds[2].LocationDisplay != "Castro 3" || exp.Sheds[2].ExperimentArm != "Sorghum pellet mix" || exp.Sheds[2].AbsoluteKg != "20.000" {
		t.Errorf("pen 3: %+v", exp.Sheds[2])
	}
	// Feed-type series: every cell is the same item, so ONE row carrying the
	// whole day's kg.
	if len(exp.Items) != 1 {
		t.Fatalf("want 1 item row, got %+v", exp.Items)
	}
	if exp.Items[0].FeedItemLabel != "Mesha TMR" || exp.Items[0].AbsoluteKg != "95.000" {
		t.Errorf("item row: want Mesha TMR 95.000, got %+v", exp.Items[0])
	}
}

// TestDirectedAnalyticsParkScopeFilter pins the scope rule on its own: a park
// the fixture never fed returns EMPTY series, never fabricated zeros, and the
// unfiltered read is unchanged by asking twice (no hidden cursor state).
func TestDirectedAnalyticsParkScopeFilter(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	cmd := ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-scope",
		IdempotencyKey: "issue:scope:" + fdiTenant, GeneratedBy: "test", Cells: analyticsCells(),
	}
	if _, err := repo.PersistIssue(ctx, cmd); err != nil {
		t.Fatalf("persist: %v", err)
	}
	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	foreign := window
	foreign.ParkIDs = []uuid.UUID{uuid.New()}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, foreign)
	if err != nil {
		t.Fatalf("foreign park: %v", err)
	}
	if len(got.Days) != 0 || len(got.Items) != 0 {
		t.Errorf("foreign park must be empty, got %+v", got)
	}
	own := window
	own.ParkIDs = []uuid.UUID{uuid.MustParse(fdiPark)}
	scoped, err := repo.DirectedAnalytics(ctx, fdiTenant, own)
	if err != nil {
		t.Fatalf("own park: %v", err)
	}
	all, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if len(scoped.Days) != 1 || len(all.Days) != 1 || scoped.Days[0] != all.Days[0] {
		t.Errorf("single-park tenant: scoped and unfiltered must agree, got %+v vs %+v", scoped.Days, all.Days)
	}

	// ParkScope for the experiment payload too (shed-wise + items, 2026-08-19):
	// a foreign park returns EMPTY series, never fabricated zeros. Pagination
	// adversarial case is deliberately absent for the experiment read — it is a
	// whole-window aggregate with no limit/offset input, same as directed
	// (pinned separately by TestDirectedAnalyticsWindowSplitPageBoundary).
	expForeign, err := repo.ExperimentAnalytics(ctx, fdiTenant, foreign)
	if err != nil {
		t.Fatalf("experiment foreign park: %v", err)
	}
	if len(expForeign.Sheds) != 0 || len(expForeign.Items) != 0 {
		t.Errorf("experiment foreign park must be empty, got %+v", expForeign)
	}
}

// TestDirectedAnalyticsWindowSplitPageBoundary pins page-size invariance the
// way this read expresses it: there IS no page input, so the whole-window
// figures must equal the union of per-day windows — splitting the window can
// never change a day's numbers.
func TestDirectedAnalyticsWindowSplitPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	for i, day := range []string{"2026-07-30", "2026-07-31"} {
		cmd := ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fmt.Sprintf("fp-split-%d", i),
			IdempotencyKey: "issue:split:" + day, GeneratedBy: "test", Cells: analyticsCells(),
		}
		if _, err := repo.PersistIssue(ctx, cmd); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
	}
	at := func(d string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", d, biztime.DefaultLocation())
		if err != nil {
			t.Fatalf("parse %s: %v", d, err)
		}
		return parsed
	}
	whole, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: at("2026-07-30"), DateTo: at("2026-07-31")})
	if err != nil {
		t.Fatalf("whole: %v", err)
	}
	if len(whole.Days) != 2 {
		t.Fatalf("want 2 days, got %+v", whole.Days)
	}
	for i, d := range []string{"2026-07-30", "2026-07-31"} {
		single, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: at(d), DateTo: at(d)})
		if err != nil {
			t.Fatalf("single %s: %v", d, err)
		}
		if len(single.Days) != 1 || single.Days[0] != whole.Days[i] {
			t.Errorf("window split changed %s: %+v vs %+v", d, single.Days, whole.Days[i])
		}
	}
}

// TestStockAnalyticsParkScopeKeepsFarmsApart pins the 2026-08-19 per-park
// stock grain: each farm has its own store, so the same feed item bought at
// two parks yields two rows whose balances never merge, depletion only
// touches the park whose locked sheet directed the kg, and a foreign
// ParkScope filter returns EMPTY items, never fabricated zeros.
func TestStockAnalyticsParkScopeKeepsFarmsApart(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	otherPark := "fd100000-0000-4000-8000-000000003002"
	if _, err := pool.Exec(ctx, `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active') ON CONFLICT (location_id) DO NOTHING`, fdiTenant, otherPark); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	purchase := func(park, farm string, qty, consumed float64, batch int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO feed_purchases
(tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date,
 quantity_kg, consumed_at_import_kg, depletes_from)
VALUES ($1::uuid, $2::uuid, $3, 'Concentrate', $4, '2026-07-01', $5, $6, '2026-07-01')`,
			fdiTenant, park, farm, batch, qty, consumed); err != nil {
			t.Fatalf("seed purchase: %v", err)
		}
	}
	purchase(fdiPark, "CBE", 100, 40, 1) // net 60 at CBE
	purchase(otherPark, "CPT", 50, 10, 2) // net 40 at CPT

	// One LOCKED sheet at CBE directs 5 kg of the item after depletes_from —
	// only the CBE balance may move.
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	cmd := ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-stock-park",
		IdempotencyKey: "issue:stockpark:" + fdiTenant, GeneratedBy: "test",
		Cells: []domain.StoredCell{{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			SessionNo: 1, SessionLabel: "S", HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", QuantityKg: kg("5.000"),
			SessionTotalKg: "5.000",
		}},
	}
	if _, err := repo.PersistIssue(ctx, cmd); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE feed_direction_issues SET state='locked', locked_at=now()
WHERE tenant_id=$1::uuid AND feed_day='2026-07-30'`, fdiTenant); err != nil {
		t.Fatalf("lock: %v", err)
	}

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.StockAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("StockAnalytics: %v", err)
	}
	byPark := map[string]domain.StockItem{}
	for _, it := range got.Items {
		if it.FeedItemKey == "concentrate" {
			byPark[it.ParkLabel] = it
		}
	}
	if len(byPark) != 2 {
		t.Fatalf("want the item once per park, got %+v", got.Items)
	}
	if byPark["CBE"].BalanceKg != "55.0" {
		t.Errorf("CBE balance: want 55.0 (net 60 minus 5 locked-directed), got %q", byPark["CBE"].BalanceKg)
	}
	if byPark["CPT"].BalanceKg != "40.0" {
		t.Errorf("CPT balance: want 40.0 (no directed depletion at that park), got %q", byPark["CPT"].BalanceKg)
	}
	if byPark["CBE"].AvgDailyKg == "" || byPark["CPT"].AvgDailyKg != "" {
		t.Errorf("avg daily must be park-scoped: CBE %q, CPT %q", byPark["CBE"].AvgDailyKg, byPark["CPT"].AvgDailyKg)
	}

	foreign := window
	foreign.ParkIDs = []uuid.UUID{uuid.New()}
	filtered, err := repo.StockAnalytics(ctx, fdiTenant, foreign)
	if err != nil {
		t.Fatalf("foreign park: %v", err)
	}
	if len(filtered.Items) != 0 {
		t.Errorf("foreign ParkScope filter: want empty stock items, got %+v", filtered.Items)
	}
}
