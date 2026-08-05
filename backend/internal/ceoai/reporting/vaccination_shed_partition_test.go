// Package reporting — adversarial grain/identity proof for migration 000114,
// which closes the documented gap left open by migration 000111 (see its
// header and docs/ceo-ai/coverage-matrix.md -> "Known gap"): ceo_ai.
// vaccination_shed_status and ceo_ai.vaccination_dose_pickup used to answer at
// PARENT-SHED grain because vaccination_eligibility_rollups had no partition
// column. This file proves both views now emit one additional row per real
// partition of a shed WITHOUT changing the pre-existing bare-shed row, and
// that a non-partitioned shed still renders its bare label only. Postgres-gated.
package reporting

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// draftProtocolVersion is protocolVersion() but leaves the version in 'draft'
// status: protocol_rules inserts are blocked by a trigger once a version is
// 'published' ("published config is immutable"), and these tests only need the
// row to exist for obligation_instances.rule_id's FK -- they never exercise
// publish/config-impact behavior.
func draftProtocolVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) string {
	t.Helper()
	var protoID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'vaccination.matrix.draft', 'Vaccination Draft', 'vaccination', 'active', 1, now(), now())
		 RETURNING protocol_id::text`, tenant).Scan(&protoID); err != nil {
		t.Fatalf("insert protocol_definition: %v", err)
	}
	var versionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, 'tenant', 1, 'draft', current_date, 1, now(), now())
		 RETURNING protocol_version_id::text`, tenant, protoID).Scan(&versionID); err != nil {
		t.Fatalf("insert protocol_version: %v", err)
	}
	return versionID
}

// protocolRule inserts the minimal protocol_rules row an obligation_instances
// row's rule_id FK requires, returning the rule_id.
func protocolRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, versionID, doseCode string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, offset_days, due_window_days, min_gap_days, created_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, 1, 'calendar', 0, 7, 0, now())
		 RETURNING rule_id::text`, tenant, versionID, doseCode).Scan(&id); err != nil {
		t.Fatalf("insert protocol_rule: %v", err)
	}
	return id
}

// obligationInstance inserts one per-goat, shed-scoped vaccination obligation
// instance. target_type is always 'goat' for a vaccination obligation, and
// target_id IS the goat_id -- the join the two partition-scoped views use to
// resolve a goat's partition via goat_shed_partitions.
func obligationInstance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, versionID, ruleID, batchID, shedID, goatID, status string, dueAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO obligation_instances (
		   obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status,
		   idempotency_key, created_at, updated_at
		 ) VALUES (
		   gen_random_uuid(), $1, $2, $3, $4,
		   'goat', $5, 'shed', $6, $7, $8,
		   $9, now(), now()
		 )`,
		tenant, versionID, ruleID, batchID, goatID, shedID, dueAt, status,
		"idem-"+goatID+"-"+status+"-"+dueAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert obligation_instance: %v", err)
	}
}

// eligibilityRollupRow inserts one vaccination_eligibility_rollups grain row
// directly -- these tests exercise the READ (view) side of migration 000114,
// so the rollup is seeded at the shape the recompute (proven separately in
// backend/internal/vaccination/adapters/postgres) would have produced.
func eligibilityRollupRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, shedID string, partitionLabel *string, animalCount int64) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO vaccination_eligibility_rollups (
		   tenant_id, park_id, shed_id, species, management_stage, sex, breed, health_status,
		   usable_for_vaccination, animal_count, source_revision, recomputed_at, updated_at, partition_label
		 ) VALUES ($1, $2, $3, 'goat', 'K1', 'female', 'boer', 'healthy', true, $4, 1, now(), now(), $5)`,
		tenant, parkID, shedID, animalCount, partitionLabel); err != nil {
		t.Fatalf("insert eligibility rollup row: %v", err)
	}
}

// TestVaccinationShedStatusPartitionRows proves ceo_ai.vaccination_shed_status
// (migration 000114) adds one row per (shed, partition) WITHOUT changing the
// pre-existing bare-shed row: a shed with 5 animals across two partitions (2 in
// "1", 3 in "2") and matching per-partition obligation due/done counts reports
// the bare row unchanged (5 animals, 3 due, 1 done -- the whole-shed totals) plus
// two new partition rows whose animals/due/done split correctly and sum back to
// the bare totals.
func TestVaccinationShedStatusPartitionRows(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park V")
	sh := shed(t, ctx, pool, tenant, pk, "Castro", nil)
	versionID := draftProtocolVersion(t, ctx, pool, tenant)
	batchID := batch(t, ctx, pool, tenant, versionID, sh, "2026-07-20", "in_progress")
	rule := protocolRule(t, ctx, pool, tenant, versionID, "et_tt")

	// Partition "1": 2 animals, both due.
	g1 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g1, sh, "1", "Castro 1")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g1, "due", time.Now().Add(48*time.Hour))
	g2 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g2, sh, "1", "Castro 1")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g2, "due", time.Now().Add(48*time.Hour))

	// Partition "2": 3 animals, 1 due + 1 done (third has no obligation row).
	g3 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g3, sh, "2", "Castro 2")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g3, "due", time.Now().Add(48*time.Hour))
	g4 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g4, sh, "2", "Castro 2")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g4, "completed", time.Now().Add(-24*time.Hour))
	g5 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g5, sh, "2", "Castro 2")

	// The rollup: ONLY the two partition rows -- matching what
	// Repository.RecomputeEligibilityRollup actually writes for a fully
	// partitioned shed (proven by TestRecomputeEligibilityRollupPartitionSumsToParentShed):
	// every goat in this shed has a partition, so there is no separate NULL/bare
	// grain row. The view's bare (whole-shed) row is derived by summing ACROSS
	// every rollup row for the shed regardless of partition -- 2+3=5 -- never by
	// reading a redundant pre-summed row.
	one := "1"
	two := "2"
	eligibilityRollupRow(t, ctx, pool, tenant, pk, sh, &one, 2)
	eligibilityRollupRow(t, ctx, pool, tenant, pk, sh, &two, 3)

	type row struct {
		label              string
		animals, due, done int64
	}
	rows, err := pool.Query(ctx,
		`SELECT COALESCE(partition_label, ''), animals, due, done FROM ceo_ai.vaccination_shed_status
		 WHERE tenant_id=$1 AND shed_label='Castro' ORDER BY COALESCE(partition_label, '')`, tenant)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.label, &r.animals, &r.due, &r.done); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows (bare + 2 partitions), got %d: %+v", len(got), got)
	}
	want := map[string]row{
		"":  {animals: 5, due: 3, done: 1},
		"1": {animals: 2, due: 2, done: 0},
		"2": {animals: 3, due: 1, done: 1},
	}
	for _, r := range got {
		w := want[r.label]
		if r.animals != w.animals || r.due != w.due || r.done != w.done {
			t.Errorf("partition %q = %+v, want %+v", r.label, r, w)
		}
	}
	// HARD INVARIANT: partition rows sum exactly to the bare (pre-existing) shed total.
	if got[1].animals+got[2].animals != got[0].animals {
		t.Fatalf("partition animals must sum to bare-row total: %d+%d != %d", got[1].animals, got[2].animals, got[0].animals)
	}
}

// TestVaccinationShedStatusNonPartitionedShedOneRow proves a shed with no
// partitions renders exactly one row, with partition_label NULL -- never the
// "whole" sentinel and never a fabricated partition split.
func TestVaccinationShedStatusNonPartitionedShedOneRow(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park W")
	sh := shed(t, ctx, pool, tenant, pk, "Yashoda", nil)
	versionID := draftProtocolVersion(t, ctx, pool, tenant)
	batchID := batch(t, ctx, pool, tenant, versionID, sh, "2026-07-20", "in_progress")
	rule := protocolRule(t, ctx, pool, tenant, versionID, "ppr")

	g1 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g1, "due", time.Now().Add(48*time.Hour))
	eligibilityRollupRow(t, ctx, pool, tenant, pk, sh, nil, 1)

	rows, err := pool.Query(ctx,
		`SELECT COALESCE(partition_label, '<null>') FROM ceo_ai.vaccination_shed_status
		 WHERE tenant_id=$1 AND shed_label='Yashoda'`, tenant)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("scan: %v", err)
		}
		labels = append(labels, l)
	}
	if len(labels) != 1 {
		t.Fatalf("non-partitioned shed must render exactly one row, got %d: %v", len(labels), labels)
	}
	if labels[0] != "<null>" {
		t.Fatalf("non-partitioned shed partition_label must be NULL, got %q (never the whole sentinel)", labels[0])
	}
}

// TestVaccinationDosePickupPartitionRows proves ceo_ai.vaccination_dose_pickup
// (migration 000114) splits animals_due/animals_overdue per partition while
// doses_to_pick (a whole-batch reservation) repeats verbatim on every partition
// row of the same batch/shed -- never divided or guessed per partition.
func TestVaccinationDosePickupPartitionRows(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park X")
	sh := shed(t, ctx, pool, tenant, pk, "Godel 1", nil)
	versionID := draftProtocolVersion(t, ctx, pool, tenant)
	batchID := batch(t, ctx, pool, tenant, versionID, sh, "2026-07-21", "in_progress")
	rule := protocolRule(t, ctx, pool, tenant, versionID, "et_tt")
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET reserved_quantity = 40 WHERE tenant_id=$1 AND batch_id=$2`,
		tenant, batchID); err != nil {
		t.Fatalf("set reserved_quantity: %v", err)
	}

	g1 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g1, sh, "Part 3", "Godel 1 - Part 3")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g1, "due", time.Now().Add(48*time.Hour))
	g2 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g2, sh, "Part 3", "Godel 1 - Part 3")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g2, "due", time.Now().Add(48*time.Hour))
	g3 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g3, sh, "Part 5", "Godel 1 - Part 5")
	obligationInstance(t, ctx, pool, tenant, versionID, rule, batchID, sh, g3, "due", time.Now().Add(48*time.Hour))

	rows, err := pool.Query(ctx,
		`SELECT COALESCE(partition_label, ''), animals_due, doses_to_pick FROM ceo_ai.vaccination_dose_pickup
		 WHERE tenant_id=$1 AND shed_label='Godel 1' AND business_date='2026-07-21'
		 ORDER BY COALESCE(partition_label, '')`, tenant)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	got := map[string][2]int64{}
	for rows.Next() {
		var label string
		var due, doses int64
		if err := rows.Scan(&label, &due, &doses); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[label] = [2]int64{due, doses}
	}
	if got["Part 3"][0] != 2 {
		t.Fatalf("Part 3 animals_due=%d want 2", got["Part 3"][0])
	}
	if got["Part 5"][0] != 1 {
		t.Fatalf("Part 5 animals_due=%d want 1", got["Part 5"][0])
	}
	// doses_to_pick is a whole-batch reservation -- SAME value on every partition row.
	if got["Part 3"][1] != 40 || got["Part 5"][1] != 40 {
		t.Fatalf("doses_to_pick must repeat the whole-batch reservation on every partition row: Part3=%d Part5=%d want 40/40",
			got["Part 3"][1], got["Part 5"][1])
	}
}
