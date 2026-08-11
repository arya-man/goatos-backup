package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestPartitionOperationalLocationMappingMovesOnlyPartitionedLiveGoats(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant            = "f1480000-0000-4000-8000-000000000001"
		custodian         = "f1480000-0000-4000-8000-000000000002"
		park              = "f1480000-0000-4000-8000-000000000003"
		godelOne          = "f1480000-0000-4000-8000-000000000004"
		castroOne         = "f1480000-0000-4000-8000-000000000005"
		partitionedGoat   = "f1480000-0000-4000-8000-000000000006"
		undividedGoat     = "f1480000-0000-4000-8000-000000000007"
		activeRetiredShed = "f1480000-0000-4000-8000-000000000008"
		isolationShed     = "f1480000-0000-4000-8000-000000000009"
		bareShed          = "f1480000-0000-4000-8000-000000000010"
		bareGoat          = "f1480000-0000-4000-8000-000000000011"
		castroGroup       = "f1480000-0000-4000-8000-000000000012"
		castroPartGoat    = "f1480000-0000-4000-8000-000000000013"
		scheduledObl      = "f1480000-0000-4000-8000-000000000014"
		waivedObl         = "f1480000-0000-4000-8000-000000000015"
		supersededObl     = "f1480000-0000-4000-8000-000000000016"
		scheduledBatch    = "f1480000-0000-4000-8000-000000000017"
		waivedBatch       = "f1480000-0000-4000-8000-000000000018"
		supersededBatch   = "f1480000-0000-4000-8000-000000000019"
		protocolVersion   = "f1480000-0000-4000-8000-000000000020"
		ruleID            = "f1480000-0000-4000-8000-000000000021"
		protocolID        = "f1480000-0000-4000-8000-000000000022"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Partition Operational Location Test', 'active')`, tenant)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Mesha Test', 'active')`, custodian)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenant, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'shed', 'Godel 1', 'active'),
       ($1::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', 'active'),
       ($1::uuid, $5::uuid, $4::uuid, 'shed', 'Bare Shed', 'active'),
       ($1::uuid, $6::uuid, $4::uuid, 'shed', 'Castro', 'active')`,
		tenant, godelOne, castroOne, park, bareShed, castroGroup)
	exec(`DROP TRIGGER IF EXISTS shed_partitions_operational_location_trg ON shed_partitions`)
	exec(`DROP FUNCTION IF EXISTS ensure_shed_partition_operational_location()`)
	exec(`ALTER TABLE shed_partitions ALTER COLUMN operational_location_id DROP NOT NULL`)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 2', '2', 'retired', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Godel 1 - Part 2', 'inactive')`,
		tenant, activeRetiredShed, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Isolation - Part 1', 'active')`,
		tenant, isolationShed, park)
	exec(`INSERT INTO goats (
  goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id,
  current_location_id, park_id, shed_id
) VALUES
  ($1::uuid, $2::uuid, 'G-900001', 'female', 'alive', $3::uuid, $4::uuid, $6::uuid, $4::uuid),
  ($5::uuid, $2::uuid, 'G-900002', 'female', 'alive', $3::uuid, $7::uuid, $6::uuid, $7::uuid),
  ($8::uuid, $2::uuid, 'G-900003', 'female', 'alive', $3::uuid, $9::uuid, $6::uuid, $9::uuid),
  ($10::uuid, $2::uuid, 'G-900004', 'female', 'alive', $3::uuid, $11::uuid, $6::uuid, $11::uuid)`,
		partitionedGoat, tenant, custodian, godelOne, undividedGoat, park, castroOne, bareGoat, bareShed, castroPartGoat, castroGroup)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'Godel 1 - Part 1')`,
		tenant, partitionedGoat, godelOne)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '1', 'Castro 1')`,
		tenant, castroPartGoat, castroGroup)
	exec(`INSERT INTO protocol_definitions (tenant_id, protocol_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'partition.obligation.test', 'Partition Obligation Test', 'vaccination', 'active')`,
		tenant, protocolID)
	exec(`INSERT INTO protocol_versions (tenant_id, protocol_version_id, protocol_id, version, version_label, status, effective_from)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'draft', '2026-01-01')`,
		tenant, protocolVersion, protocolID)
	exec(`INSERT INTO protocol_rules (tenant_id, rule_id, protocol_version_id, dose_code, trigger_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'partition-test-dose', 'manual_campaign')`,
		tenant, ruleID, protocolVersion)
	exec(`INSERT INTO obligation_batches (tenant_id, batch_id, protocol_version_id, scope_type, scope_id, status, session)
VALUES
  ($1::uuid, $2::uuid, $5::uuid, 'shed', $6::uuid, 'planned', 'scheduled'),
  ($1::uuid, $3::uuid, $5::uuid, 'shed', $6::uuid, 'planned', 'waived'),
  ($1::uuid, $4::uuid, $5::uuid, 'shed', $6::uuid, 'superseded', 'superseded')`,
		tenant, scheduledBatch, waivedBatch, supersededBatch, protocolVersion, godelOne)
	exec(`INSERT INTO obligation_instances (
	  tenant_id, obligation_id, protocol_version_id, rule_id, batch_id,
	  target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence
	) VALUES
	  ($1::uuid, $2::uuid, $8::uuid, $9::uuid, $5::uuid, 'goat', $10::uuid, 'shed', $11::uuid, '2026-01-01 09:00:00+00', 'scheduled', 'partition-obligation-scheduled', 1),
	  ($1::uuid, $3::uuid, $8::uuid, $9::uuid, $6::uuid, 'goat', $10::uuid, 'shed', $11::uuid, '2026-01-02 09:00:00+00', 'waived', 'partition-obligation-waived', 2),
	  ($1::uuid, $4::uuid, $8::uuid, $9::uuid, $7::uuid, 'goat', $10::uuid, 'shed', $11::uuid, '2026-01-03 09:00:00+00', 'superseded', 'partition-obligation-superseded', 3)`,
		tenant, scheduledObl, waivedObl, supersededObl, scheduledBatch, waivedBatch, supersededBatch, protocolVersion, ruleID, partitionedGoat, godelOne)

	raw, err := os.ReadFile("000152_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000152: %v", err)
	}
	goatBackfillRaw, err := os.ReadFile("000154_partition_goat_residence_backfill.sql")
	if err != nil {
		t.Fatalf("read goat residence backfill migration: %v", err)
	}
	runNoTransactionMigration(t, ctx, pool, migrationUp(string(goatBackfillRaw)))

	var currentLocationID, shedID, shedGroupID, currentType, parentID string
	if err := pool.QueryRow(ctx, `
SELECT g.current_location_id::text, g.shed_id::text, COALESCE(g.shed_group_id::text, ''), cur.location_type, cur.parent_location_id::text
FROM goats g
JOIN locations cur ON cur.tenant_id=g.tenant_id AND cur.location_id=g.current_location_id
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid`, tenant, partitionedGoat).
		Scan(&currentLocationID, &shedID, &shedGroupID, &currentType, &parentID); err != nil {
		t.Fatalf("query partitioned goat: %v", err)
	}
	if currentLocationID == godelOne {
		t.Fatalf("partitioned goat current_location_id stayed on parent shed %s", godelOne)
	}
	if currentLocationID == isolationShed {
		t.Fatalf("partitioned goat current_location_id reused unrelated numbered shed %s", isolationShed)
	}
	if shedID != currentLocationID {
		t.Fatalf("partitioned goat shed_id=%s, want exact partition location %s", shedID, currentLocationID)
	}
	if shedGroupID != godelOne {
		t.Fatalf("partitioned goat shed_group_id=%s, want parent group %s", shedGroupID, godelOne)
	}
	if currentType != "shed" || parentID != park {
		t.Fatalf("partitioned goat current location type/parent = %s/%s, want shed/%s", currentType, parentID, park)
	}
	var castroCurrentLocationID, castroShedID, castroGroupID, castroCurrentName string
	if err := pool.QueryRow(ctx, `
SELECT g.current_location_id::text, g.shed_id::text, COALESCE(g.shed_group_id::text, ''), cur.name
FROM goats g
JOIN locations cur ON cur.tenant_id=g.tenant_id AND cur.location_id=g.current_location_id
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid`, tenant, castroPartGoat).
		Scan(&castroCurrentLocationID, &castroShedID, &castroGroupID, &castroCurrentName); err != nil {
		t.Fatalf("query Castro source-name goat: %v", err)
	}
	if castroCurrentLocationID != castroOne || castroShedID != castroOne || castroGroupID != castroGroup || castroCurrentName != "Castro 1" {
		t.Fatalf("Castro source-name goat mapped to current=%s shed=%s group=%s name=%q; want exact Castro 1=%s with group Castro=%s",
			castroCurrentLocationID, castroShedID, castroGroupID, castroCurrentName, castroOne, castroGroup)
	}
	var penName string
	if err := pool.QueryRow(ctx, `
SELECT l.name
FROM goats g
JOIN locations l ON l.tenant_id=g.tenant_id AND l.location_id=g.current_location_id
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid`, tenant, partitionedGoat).
		Scan(&penName); err != nil {
		t.Fatalf("query partitioned goat location name: %v", err)
	}
	if penName != "Godel 1 - Part 1" {
		t.Fatalf("partitioned goat mapped to shed name %q, want %q", penName, "Godel 1 - Part 1")
	}

	var scheduledScope, waivedScope, supersededScope string
	if err := pool.QueryRow(ctx, `
SELECT
  max(scope_id::text) FILTER (WHERE obligation_id=$2::uuid),
  max(scope_id::text) FILTER (WHERE obligation_id=$3::uuid),
  max(scope_id::text) FILTER (WHERE obligation_id=$4::uuid)
FROM obligation_instances
WHERE tenant_id=$1::uuid`, tenant, scheduledObl, waivedObl, supersededObl).
		Scan(&scheduledScope, &waivedScope, &supersededScope); err != nil {
		t.Fatalf("query obligation scopes after exact shed backfill: %v", err)
	}
	if scheduledScope != currentLocationID {
		t.Fatalf("scheduled obligation scope=%s, want exact shed %s", scheduledScope, currentLocationID)
	}
	if waivedScope != godelOne || supersededScope != godelOne {
		t.Fatalf("terminal obligation scopes waived=%s superseded=%s, want both preserved on parent %s", waivedScope, supersededScope, godelOne)
	}

	if err := pool.QueryRow(ctx, `
SELECT current_location_id::text, shed_id::text
FROM goats
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, undividedGoat).
		Scan(&currentLocationID, &shedID); err != nil {
		t.Fatalf("query undivided goat: %v", err)
	}
	if currentLocationID != castroOne || shedID != castroOne {
		t.Fatalf("undivided goat got remapped to current=%s shed=%s, want both %s", currentLocationID, shedID, castroOne)
	}

	var retiredShedID, retiredShedStatus string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.status
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='2'`,
		tenant, godelOne).Scan(&retiredShedID, &retiredShedStatus); err != nil {
		t.Fatalf("query retired partition mapping: %v", err)
	}
	if retiredShedID == "" || retiredShedStatus != "inactive" {
		t.Fatalf("retired partition mapped to shed=%q status=%q, want inactive shed mapping", retiredShedID, retiredShedStatus)
	}
	if retiredShedID != activeRetiredShed {
		t.Fatalf("retired partition mapped to %s; want existing inactive shed %s", retiredShedID, activeRetiredShed)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, bareShed); err == nil {
		t.Fatal("expected first active partition over live bare-shed goat to fail")
	} else if !strings.Contains(err.Error(), "shed_partition_activation_bare_residents") {
		t.Fatalf("unexpected bare-shed activation error: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE shed_partitions
SET shed_id=$3::uuid
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND normalized_label='1'`,
		tenant, godelOne, castroOne); err == nil {
		t.Fatal("expected occupied partition reparent to fail")
	} else if !strings.Contains(err.Error(), "shed_partition_operational_location_in_use") {
		t.Fatalf("unexpected occupied reparent error: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE shed_partitions
SET partition_label='Part 1A', normalized_label='1a'
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND normalized_label='1'`,
		tenant, godelOne); err == nil {
		t.Fatal("expected occupied partition rename to fail")
	} else if !strings.Contains(err.Error(), "shed_partition_operational_location_in_use") {
		t.Fatalf("unexpected occupied rename error: %v", err)
	}

	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual')`, tenant, godelOne)
	var futureShedID, futureShedStatus string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.status
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='3'`,
		tenant, godelOne).Scan(&futureShedID, &futureShedStatus); err != nil {
		t.Fatalf("query future active partition mapping: %v", err)
	}
	if futureShedID == "" || futureShedStatus != "active" {
		t.Fatalf("future active partition mapped to shed=%q status=%q, want active shed mapping", futureShedID, futureShedStatus)
	}
	exec(`UPDATE shed_partitions
SET shed_id=$3::uuid
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND normalized_label='3'`,
		tenant, godelOne, castroOne)
	var reparentedShedID, reparentedShedParent string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.parent_location_id::text
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='3'`,
		tenant, castroOne).Scan(&reparentedShedID, &reparentedShedParent); err != nil {
		t.Fatalf("query reparented active partition mapping: %v", err)
	}
	if reparentedShedID == futureShedID || reparentedShedParent != park {
		t.Fatalf("reparented partition kept stale shed=%s parent=%s; want new partition shed under park %s", reparentedShedID, reparentedShedParent, park)
	}

	exec(`UPDATE goats SET lifecycle_status='sold' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, partitionedGoat)
	runNoTransactionMigration(t, ctx, pool, migrationDown(string(goatBackfillRaw)))
	if _, err := pool.Exec(ctx, migrationDown(string(raw))); err != nil {
		t.Fatalf("rollback 000152: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT current_location_id::text
FROM goats
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, partitionedGoat).Scan(&currentLocationID); err != nil {
		t.Fatalf("query terminal goat after rollback: %v", err)
	}
	if currentLocationID != godelOne {
		t.Fatalf("terminal goat current_location_id after rollback=%s, want parent shed %s", currentLocationID, godelOne)
	}
	var helperExists bool
	if err := pool.QueryRow(ctx, `
SELECT to_regprocedure('public.operational_location_display(text,text)') IS NOT NULL`).Scan(&helperExists); err != nil {
		t.Fatalf("query helper function existence after rollback: %v", err)
	}
	if helperExists {
		t.Fatalf("helper function public.operational_location_display(text,text) still exists after rollback")
	}
}

func TestPartitionOperationalLocationMigrationUpDownUpKeepsOneActivePen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant = "f1460000-0000-4000-8000-000000000001"
		park   = "f1460000-0000-4000-8000-000000000002"
		shed   = "f1460000-0000-4000-8000-000000000003"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Partition Migration Cycle Test', 'active')`, tenant)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenant, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Godel 1', 'active')`, tenant, shed, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Godel 1 - Part 1', 'active')`, tenant, "f1460000-0000-4000-8000-000000000010", park)

	raw, err := os.ReadFile("000152_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("migrate up failed: %v", err)
	}
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, shed)

	if _, err := pool.Exec(ctx, migrationDown(string(raw))); err != nil {
		t.Fatalf("migrate down failed: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("migrate up after down failed: %v", err)
	}

	var partitionShedCount int
	if err := pool.QueryRow(ctx, `
	SELECT count(*)
	FROM locations
	WHERE tenant_id=$1::uuid
	  AND parent_location_id=$2::uuid
	  AND location_type='shed'
	  AND name = operational_location_display('Godel 1', 'Part 1')
	  AND status='active'`, tenant, park).Scan(&partitionShedCount); err != nil {
		t.Fatalf("query partition shed count failed: %v", err)
	}
	if partitionShedCount != 1 {
		t.Fatalf("expected one active mapped partition shed after up-down-up, got %d", partitionShedCount)
	}

	var spShedID string
	if err := pool.QueryRow(ctx, `
	SELECT sp.operational_location_id::text
	FROM shed_partitions sp
	WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='1'`, tenant, shed).
		Scan(&spShedID); err != nil {
		t.Fatalf("query partition mapping failed: %v", err)
	}
	var partitionShedID string
	if err := pool.QueryRow(ctx, `
	SELECT location_id::text
	FROM locations
	WHERE tenant_id=$1::uuid AND parent_location_id=$2::uuid
	  AND name = operational_location_display('Godel 1', 'Part 1')
	  AND location_type='shed'`, tenant, park).Scan(&partitionShedID); err != nil {
		t.Fatalf("query partition shed id failed: %v", err)
	}
	if spShedID != partitionShedID {
		t.Fatalf("partition mapped to %s, expected %s", spShedID, partitionShedID)
	}
}

func TestPartitionGoatResidenceBackfillIsSeparatedFromTransactionalSchemaMigration(t *testing.T) {
	raw152, err := os.ReadFile("000152_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read 000152: %v", err)
	}
	up152 := migrationUp(string(raw152))
	down152 := migrationDown(string(raw152))
	for _, forbidden := range []string{
		"SET lock_timeout = '2s';\nUPDATE public.goats g\nSET current_location_id = sp.operational_location_id",
		"SET lock_timeout = '2s';\nUPDATE public.goats g\nSET current_location_id = g.shed_group_id",
	} {
		if strings.Contains(up152, forbidden) || strings.Contains(down152, forbidden) {
			t.Fatal("000152 must not run the one-time hot goats-table rewrite; use the chunked no-transaction data migration")
		}
	}

	raw154, err := os.ReadFile("000154_partition_goat_residence_backfill.sql")
	if err != nil {
		t.Fatalf("read 000154: %v", err)
	}
	migration154 := string(raw154)
	for _, required := range []string{
		"-- +goose NO TRANSACTION",
		"FOR UPDATE OF g SKIP LOCKED",
		"LIMIT GREATEST(p_batch_size, 1)",
		"GET DIAGNOSTICS moved_count = ROW_COUNT",
		"COMMIT",
	} {
		if !strings.Contains(migration154, required) {
			t.Fatalf("000154 chunked goat residence backfill missing %q", required)
		}
	}
}

func TestShedPartitionOperationalLocationTriggerRejectsInvalidAndSerializes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant         = "f1490000-0000-4000-8000-000000000001"
		park           = "f1490000-0000-4000-8000-000000000002"
		activeShed     = "f1490000-0000-4000-8000-000000000003"
		inactiveShed   = "f1490000-0000-4000-8000-000000000004"
		activePartShed = "f1490000-0000-4000-8000-000000000005"
		concurrentShed = "f1490000-0000-4000-8000-000000000006"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}
	expectErr := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("expected exec to fail\nsql: %s", sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Partition Trigger Test', 'active')`, tenant)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenant, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $5::uuid, 'shed', 'Valid Shed', 'active'),
       ($1::uuid, $3::uuid, $5::uuid, 'shed', 'Inactive Shed', 'inactive'),
       ($1::uuid, $4::uuid, $5::uuid, 'shed', 'Concurrent Shed', 'active')`,
		tenant, activeShed, inactiveShed, concurrentShed, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Valid Shed - Part 9', 'active')`,
		tenant, activePartShed, park)

	expectErr(`INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES ($1::uuid, $2::uuid, 'Part Parent', 'parent', 'active', 'manual', $2::uuid)`,
		tenant, activeShed)
	expectErr(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, inactiveShed)
	expectErr(`INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
	) VALUES ($1::uuid, $2::uuid, 'Part 9', '9', 'retired', 'manual', $3::uuid)`,
		tenant, activeShed, activePartShed)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := pool.Exec(ctx, `INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source
) VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')
ON CONFLICT DO NOTHING`,
				tenant, concurrentShed)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent partition insert failed: %v", err)
		}
	}

	var partitionShedCount int
	if err := pool.QueryRow(ctx, `
	SELECT count(*)
	FROM locations
	WHERE tenant_id=$1::uuid
	  AND parent_location_id=$2::uuid
	  AND location_type='shed'
	  AND status='active'
	  AND lower(name)=lower('Concurrent Shed - Part 1')`,
		tenant, park).Scan(&partitionShedCount); err != nil {
		t.Fatalf("query concurrent partition sheds: %v", err)
	}
	if partitionShedCount != 1 {
		t.Fatalf("concurrent partition insert created %d partition sheds, want exactly 1", partitionShedCount)
	}
}

func TestShedPartitionOperationalLocationTriggerRetiresFailWhilePlacementInFlight(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant     = "f14a0000-0000-4000-8000-000000000001"
		custodian  = "f14a0000-0000-4000-8000-000000000002"
		park       = "f14a0000-0000-4000-8000-000000000003"
		shed       = "f14a0000-0000-4000-8000-000000000004"
		goatID     = "f14a0000-0000-4000-8000-000000000005"
		normalized = "10"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Partition Retire Race Test', 'active')`, tenant)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Ravi Test', 'active')`, custodian)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, tenant, park)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Godel 9', 'active')`,
		tenant, shed, park)
	exec(`INSERT INTO goats (
  goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id, current_location_id, park_id, shed_id
) VALUES
  ($1::uuid, $2::uuid, 'G-900010', 'female', 'alive', $3::uuid, NULL, $5::uuid, $4::uuid)`,
		goatID, tenant, custodian, shed, park)

	raw, err := os.ReadFile("000152_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000152: %v", err)
	}

	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 10', $3::text, 'active', 'manual')`, tenant, shed, normalized)

	var partitionShedID string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text
FROM shed_partitions sp
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label=$3::text
  AND sp.status='active'`,
		tenant, shed, normalized).Scan(&partitionShedID); err != nil {
		t.Fatalf("query partition shed: %v", err)
	}

	placementReady := make(chan struct{})
	placementReadyToContinue := make(chan struct{})
	placementPID := make(chan int, 1)
	retirementPID := make(chan int, 1)
	retireErrCh := make(chan error, 1)
	placeErrCh := make(chan error, 1)
	var closePlacementReadyToContinue sync.Once
	signalPlacementToContinue := func() {
		closePlacementReadyToContinue.Do(func() {
			close(placementReadyToContinue)
		})
	}
	defer signalPlacementToContinue()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, err := pool.Begin(ctx)
		if err != nil {
			placeErrCh <- err
			return
		}
		var lockedPartitionShedID string
		if err := tx.QueryRow(ctx, `
SELECT sp.operational_location_id::text
FROM shed_partitions sp
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label=$3::text AND sp.status='active'
FOR SHARE OF sp`,
			tenant, shed, normalized).Scan(&lockedPartitionShedID); err != nil {
			_ = tx.Rollback(ctx)
			placeErrCh <- err
			return
		}
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(ctx)
			placeErrCh <- err
			return
		}
		placementPID <- pid
		close(placementReady)
		<-placementReadyToContinue
		if _, err := tx.Exec(ctx, `UPDATE goats
SET current_location_id = $1::uuid
WHERE tenant_id=$2::uuid AND goat_id=$3::uuid`,
			lockedPartitionShedID, tenant, goatID); err != nil {
			_ = tx.Rollback(ctx)
			placeErrCh <- err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			placeErrCh <- err
			return
		}
		placeErrCh <- nil
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-placementReady
		tx, err := pool.Begin(ctx)
		if err != nil {
			retireErrCh <- err
			return
		}
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(ctx)
			retireErrCh <- err
			return
		}
		retirementPID <- pid

		_, err = tx.Exec(ctx, `
UPDATE shed_partitions
SET status='retired'
WHERE tenant_id=$1::uuid
  AND shed_id=$2::uuid
  AND normalized_label=$3::text
  AND status='active'`, tenant, shed, normalized)
		if err == nil {
			_ = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		retireErrCh <- err
	}()

	select {
	case <-placementReady:
	case err := <-placeErrCh:
		t.Fatalf("placement setup failed: %v", err)
	}
	placePid, ok := <-placementPID
	if !ok {
		t.Fatal("missing placement pid")
	}
	retireSetupDeadline := time.Now().Add(5 * time.Second)
	var retirePid int
	select {
	case err := <-retireErrCh:
		if err == nil {
			t.Fatal("retirement finished before placement lock was held")
		}
		t.Fatalf("retirement setup failed: %v", err)
	case retirePid = <-retirementPID:
		// expected path: retired transaction now blocked on placement lock
	case <-time.After(time.Until(retireSetupDeadline)):
		t.Fatal("retirement did not start")
	}

	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var isBlocked bool
		if err := pool.QueryRow(ctx, `
SELECT $2::int = ANY(pg_blocking_pids($1::int))
`, retirePid, placePid).Scan(&isBlocked); err != nil {
			t.Fatalf("query blocking pids: %v", err)
		}
		if isBlocked {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("retirement transaction was not blocked by placement lock")
	}

	signalPlacementToContinue()
	wg.Wait()
	close(placeErrCh)
	close(retireErrCh)

	placementErr := <-placeErrCh
	if placementErr != nil {
		t.Fatalf("placement transaction failed: %v", placementErr)
	}

	retireErr, ok := <-retireErrCh
	if !ok || retireErr == nil {
		t.Fatalf("retirement transaction unexpectedly succeeded; expected shed_partition_operational_location_in_use")
	}
	if !strings.Contains(retireErr.Error(), "shed_partition_operational_location_in_use") {
		t.Fatalf("unexpected retirement error: %v", retireErr)
	}

	var partitionStatus string
	if err := pool.QueryRow(ctx, `
SELECT status
FROM shed_partitions
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND normalized_label=$3::text`,
		tenant, shed, normalized).Scan(&partitionStatus); err != nil {
		t.Fatalf("query partition status after race: %v", err)
	}
	if partitionStatus != "active" {
		t.Fatalf("partition status after race=%q, want active", partitionStatus)
	}

	var finalLocation string
	if err := pool.QueryRow(ctx, `
SELECT current_location_id::text
FROM goats
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, goatID).Scan(&finalLocation); err != nil {
		t.Fatalf("query final goat location: %v", err)
	}
	if finalLocation != partitionShedID {
		t.Fatalf("goat ended at %q, want partition shed %q", finalLocation, partitionShedID)
	}
}

func migrationDown(sqlText string) string {
	parts := strings.SplitN(sqlText, "-- +goose Down", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
