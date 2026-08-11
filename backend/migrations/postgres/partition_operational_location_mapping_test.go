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
		tenant           = "f1480000-0000-4000-8000-000000000001"
		custodian        = "f1480000-0000-4000-8000-000000000002"
		park             = "f1480000-0000-4000-8000-000000000003"
		godelOne         = "f1480000-0000-4000-8000-000000000004"
		castroOne        = "f1480000-0000-4000-8000-000000000005"
		partitionedGoat  = "f1480000-0000-4000-8000-000000000006"
		undividedGoat    = "f1480000-0000-4000-8000-000000000007"
		activeRetiredPen = "f1480000-0000-4000-8000-000000000008"
		isolationPen     = "f1480000-0000-4000-8000-000000000009"
		bareShed         = "f1480000-0000-4000-8000-000000000010"
		bareGoat         = "f1480000-0000-4000-8000-000000000011"
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
       ($1::uuid, $5::uuid, $4::uuid, 'shed', 'Bare Shed', 'active')`,
		tenant, godelOne, castroOne, park, bareShed)
	exec(`DROP TRIGGER IF EXISTS shed_partitions_operational_location_trg ON shed_partitions`)
	exec(`DROP FUNCTION IF EXISTS ensure_shed_partition_operational_location()`)
	exec(`ALTER TABLE shed_partitions ALTER COLUMN operational_location_id DROP NOT NULL`)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 2', '2', 'retired', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Godel 1 - Part 2', 'active')`,
		tenant, activeRetiredPen, godelOne)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Isolation - Part 1', 'active')`,
		tenant, isolationPen, godelOne)
	exec(`INSERT INTO goats (
  goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id,
  current_location_id, park_id, shed_id
) VALUES
  ($1::uuid, $2::uuid, 'G-900001', 'female', 'alive', $3::uuid, $4::uuid, $6::uuid, $4::uuid),
  ($5::uuid, $2::uuid, 'G-900002', 'female', 'alive', $3::uuid, $7::uuid, $6::uuid, $7::uuid),
  ($8::uuid, $2::uuid, 'G-900003', 'female', 'alive', $3::uuid, $9::uuid, $6::uuid, $9::uuid)`,
		partitionedGoat, tenant, custodian, godelOne, undividedGoat, park, castroOne, bareGoat, bareShed)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'Godel 1 - Part 1')`,
		tenant, partitionedGoat, godelOne)

	raw, err := os.ReadFile("000152_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000152: %v", err)
	}

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
	if currentLocationID == isolationPen {
		t.Fatalf("partitioned goat current_location_id reused unrelated numbered pen %s", isolationPen)
	}
	if shedID != currentLocationID {
		t.Fatalf("partitioned goat shed_id=%s, want exact partition location %s", shedID, currentLocationID)
	}
	if shedGroupID != godelOne {
		t.Fatalf("partitioned goat shed_group_id=%s, want parent group %s", shedGroupID, godelOne)
	}
	if currentType != "pen" || parentID != godelOne {
		t.Fatalf("partitioned goat current location type/parent = %s/%s, want pen/%s", currentType, parentID, godelOne)
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
		t.Fatalf("partitioned goat mapped to pen name %q, want %q", penName, "Godel 1 - Part 1")
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

	var retiredPenID, retiredPenStatus string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.status
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='2'`,
		tenant, godelOne).Scan(&retiredPenID, &retiredPenStatus); err != nil {
		t.Fatalf("query retired partition mapping: %v", err)
	}
	if retiredPenID == "" || retiredPenStatus != "inactive" {
		t.Fatalf("retired partition mapped to pen=%q status=%q, want inactive pen mapping", retiredPenID, retiredPenStatus)
	}
	if retiredPenID == activeRetiredPen {
		t.Fatalf("retired partition reused active pen %s; want a separate inactive mapping", activeRetiredPen)
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
	var futurePenID, futurePenStatus string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.status
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='3'`,
		tenant, godelOne).Scan(&futurePenID, &futurePenStatus); err != nil {
		t.Fatalf("query future active partition mapping: %v", err)
	}
	if futurePenID == "" || futurePenStatus != "active" {
		t.Fatalf("future active partition mapped to pen=%q status=%q, want active pen mapping", futurePenID, futurePenStatus)
	}
	exec(`UPDATE shed_partitions
SET shed_id=$3::uuid
WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND normalized_label='3'`,
		tenant, godelOne, castroOne)
	var reparentedPenID, reparentedPenParent string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text, pen.parent_location_id::text
FROM shed_partitions sp
JOIN locations pen ON pen.tenant_id=sp.tenant_id AND pen.location_id=sp.operational_location_id
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='3'`,
		tenant, castroOne).Scan(&reparentedPenID, &reparentedPenParent); err != nil {
		t.Fatalf("query reparented active partition mapping: %v", err)
	}
	if reparentedPenID == futurePenID || reparentedPenParent != castroOne {
		t.Fatalf("reparented partition kept stale pen=%s parent=%s; want new pen under %s", reparentedPenID, reparentedPenParent, castroOne)
	}

	exec(`UPDATE goats SET lifecycle_status='sold' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, partitionedGoat)
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
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Godel 1 - Part 1', 'active')`, tenant, "f1460000-0000-4000-8000-000000000010", shed)

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

	var penCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM locations
WHERE tenant_id=$1::uuid
  AND parent_location_id=$2::uuid
  AND location_type='pen'
  AND name = operational_location_display('Godel 1', 'Part 1')
  AND status='active'`, tenant, shed).Scan(&penCount); err != nil {
		t.Fatalf("query pen count failed: %v", err)
	}
	if penCount != 1 {
		t.Fatalf("expected one active mapped pen after up-down-up, got %d", penCount)
	}

	var spPenID string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text
FROM shed_partitions sp
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label='1'`, tenant, shed).
		Scan(&spPenID); err != nil {
		t.Fatalf("query partition mapping failed: %v", err)
	}
	var penID string
	if err := pool.QueryRow(ctx, `
SELECT location_id::text
FROM locations
WHERE tenant_id=$1::uuid AND parent_location_id=$2::uuid
  AND name = operational_location_display('Godel 1', 'Part 1')
  AND location_type='pen'`, tenant, shed).Scan(&penID); err != nil {
		t.Fatalf("query pen id failed: %v", err)
	}
	if spPenID != penID {
		t.Fatalf("partition mapped to %s, expected %s", spPenID, penID)
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
		activePen      = "f1490000-0000-4000-8000-000000000005"
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
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Valid Shed - Part 9', 'active')`,
		tenant, activePen, activeShed)

	expectErr(`INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES ($1::uuid, $2::uuid, 'Part Parent', 'parent', 'active', 'manual', $2::uuid)`,
		tenant, activeShed)
	expectErr(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, inactiveShed)
	expectErr(`INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES ($1::uuid, $2::uuid, 'Part 9', '9', 'retired', 'manual', $3::uuid)`,
		tenant, activeShed, activePen)

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

	var penCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM locations
WHERE tenant_id=$1::uuid
  AND parent_location_id=$2::uuid
  AND location_type='pen'
  AND status='active'
  AND lower(name)=lower('Concurrent Shed - Part 1')`,
		tenant, concurrentShed).Scan(&penCount); err != nil {
		t.Fatalf("query concurrent pens: %v", err)
	}
	if penCount != 1 {
		t.Fatalf("concurrent partition insert created %d pens, want exactly 1", penCount)
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

	var penID string
	if err := pool.QueryRow(ctx, `
SELECT sp.operational_location_id::text
FROM shed_partitions sp
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label=$3::text
  AND sp.status='active'`,
		tenant, shed, normalized).Scan(&penID); err != nil {
		t.Fatalf("query partition pen: %v", err)
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
		var lockedPenID string
		if err := tx.QueryRow(ctx, `
SELECT sp.operational_location_id::text
FROM shed_partitions sp
WHERE sp.tenant_id=$1::uuid AND sp.shed_id=$2::uuid AND sp.normalized_label=$3::text AND sp.status='active'
FOR SHARE OF sp`,
			tenant, shed, normalized).Scan(&lockedPenID); err != nil {
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
			lockedPenID, tenant, goatID); err != nil {
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
	if finalLocation != penID {
		t.Fatalf("goat ended at %q, want partition pen %q", finalLocation, penID)
	}
}

func migrationDown(sqlText string) string {
	parts := strings.SplitN(sqlText, "-- +goose Down", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
