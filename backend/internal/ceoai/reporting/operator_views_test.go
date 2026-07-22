// Package reporting — adversarial grain/identity proof for
// ceo_ai.vaccination_operator_status (migration 000026), the OPERATOR-grain
// vaccination drive view over the operator-based drive model. Proves the
// batch-status FILTER never fans out animal_count (assignment→batch is 1:1),
// that per-operator-per-day capacity is not multiplied across shed rows, and
// that overdue/utilization/next_action are derived correctly. Postgres-gated.
package reporting

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// protocolVersion creates the minimal protocol_definition + protocol_version an
// obligation_batch requires, returning the protocol_version_id.
func protocolVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) string {
	t.Helper()
	var protoID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'vaccination.matrix', 'Vaccination', 'vaccination', 'active', 1, now(), now())
		 RETURNING protocol_id::text`, tenant).Scan(&protoID); err != nil {
		t.Fatalf("insert protocol_definition: %v", err)
	}
	var versionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, 'tenant', 1, 'active', current_date, 1, now(), now())
		 RETURNING protocol_version_id::text`, tenant, protoID).Scan(&versionID); err != nil {
		t.Fatalf("insert protocol_version: %v", err)
	}
	return versionID
}

func batch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, versionID, shedID, plannedDate, status string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, planned_date, status, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, 'shed', $3, $4::date, $5, 1, now(), now())
		 RETURNING batch_id::text`, tenant, versionID, shedID, plannedDate, status).Scan(&id); err != nil {
		t.Fatalf("insert obligation_batch: %v", err)
	}
	return id
}

func operatorMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, metadata, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, encode(gen_random_bytes(4),'hex'), $2, 'active', 'operator', '{}'::jsonb, 1, now(), now())
		 RETURNING workforce_member_id::text`, tenant, name).Scan(&id); err != nil {
		t.Fatalf("insert operator member: %v", err)
	}
	return id
}

func assignment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, batchID, operatorID, parkID, shedID, physicalShed, partition, plannedDate string, animals int) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3::date, $4, $5, $6, $7, $8, $9, now(), now())`,
		tenant, batchID, plannedDate, operatorID, parkID, shedID, physicalShed, partition, animals); err != nil {
		t.Fatalf("insert drive assignment: %v", err)
	}
}

// TestOperatorStatusGrainAndCapacity proves: (1) an operator assigned across TWO
// sheds in ONE day sums assigned_animals but does NOT multiply the per-day
// capacity; (2) an overdue (past planned_date, still-open batch) is counted as
// overdue and a completed batch is counted as done, with no double count from
// the batch join; (3) utilization = day total / cap and next_action reflects it.
func TestOperatorStatusGrainAndCapacity(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	// tenant per-operator-per-day cap = 10 animals.
	if _, err := pool.Exec(ctx,
		`INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days)
		 VALUES ($1, 10, 'tenant', 7)`, tenant); err != nil {
		t.Fatalf("insert capacity config: %v", err)
	}

	pk := park(t, ctx, pool, tenant, "Castro 1")
	shedA := shed(t, ctx, pool, tenant, pk, "Shed A1", nil)
	shedB := shed(t, ctx, pool, tenant, pk, "Shed B1", nil)
	ver := protocolVersion(t, ctx, pool, tenant)
	op := operatorMember(t, ctx, pool, tenant, "Ramesh")

	// Two OPEN (planned) batches on a PAST date => overdue; across two sheds same
	// day. Assigned 8 + 6 = 14 > cap 10 => overloaded, and both overdue.
	past := "2020-01-01"
	bA := batch(t, ctx, pool, tenant, ver, shedA, past, "planned")
	bB := batch(t, ctx, pool, tenant, ver, shedB, past, "in_progress")
	assignment(t, ctx, pool, tenant, bA, op, pk, shedA, "Shed A", "whole", past, 8)
	assignment(t, ctx, pool, tenant, bB, op, pk, shedB, "Shed B", "whole", past, 6)

	type row struct {
		shed        string
		assigned    int64
		due         int64
		done        int64
		overdue     int64
		capacity    int64
		dayAssigned int64
		util        float64
		nextAction  string
	}
	rows, err := pool.Query(ctx,
		`SELECT shed_label, assigned_animals, due, done, overdue, daily_capacity,
		        operator_day_assigned, utilization, next_action
		 FROM ceo_ai.vaccination_operator_status
		 WHERE tenant_id=$1 AND operator_id=$2 ORDER BY shed_label`, tenant, op)
	if err != nil {
		t.Fatalf("query operator status: %v", err)
	}
	defer rows.Close()
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.shed, &r.assigned, &r.due, &r.done, &r.overdue, &r.capacity, &r.dayAssigned, &r.util, &r.nextAction); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 shed rows, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		// capacity is the per-operator-per-day cap on EVERY row, never multiplied.
		if r.capacity != 10 {
			t.Errorf("%s: daily_capacity=%d want 10", r.shed, r.capacity)
		}
		// the operator's whole-day assigned total is 14 on every row (window sum).
		if r.dayAssigned != 14 {
			t.Errorf("%s: operator_day_assigned=%d want 14", r.shed, r.dayAssigned)
		}
		// utilization = 14/10 = 1.4 => overloaded.
		if r.util < 1.39 || r.util > 1.41 {
			t.Errorf("%s: utilization=%v want ~1.4", r.shed, r.util)
		}
		// overdue wins next_action (past date, still-open batch).
		if r.overdue == 0 {
			t.Errorf("%s: overdue=0 want >0", r.shed)
		}
		if r.nextAction != "catch_up_overdue" {
			t.Errorf("%s: next_action=%q want catch_up_overdue", r.shed, r.nextAction)
		}
		if r.done != 0 {
			t.Errorf("%s: done=%d want 0 (no completed batch)", r.shed, r.done)
		}
	}

	// A completed batch on the SAME day for another operator counts as done, not
	// overdue — proves the batch-status FILTER partitions correctly.
	op2 := operatorMember(t, ctx, pool, tenant, "Sita")
	bC := batch(t, ctx, pool, tenant, ver, shedA, past, "completed")
	assignment(t, ctx, pool, tenant, bC, op2, pk, shedA, "Shed A", "whole", past, 5)
	var done, overdue int64
	var action string
	if err := pool.QueryRow(ctx,
		`SELECT done, overdue, next_action FROM ceo_ai.vaccination_operator_status
		 WHERE tenant_id=$1 AND operator_id=$2`, tenant, op2).Scan(&done, &overdue, &action); err != nil {
		t.Fatalf("query op2: %v", err)
	}
	if done != 5 || overdue != 0 {
		t.Errorf("op2 done=%d overdue=%d want done=5 overdue=0", done, overdue)
	}
	if action != "completed" {
		t.Errorf("op2 next_action=%q want completed", action)
	}
}
