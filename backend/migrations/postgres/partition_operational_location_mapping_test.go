package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

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
       ($1::uuid, $3::uuid, $4::uuid, 'shed', 'Castro 1', 'active')`,
		tenant, godelOne, castroOne, park)
	exec(`ALTER TABLE shed_partitions ALTER COLUMN operational_location_id DROP NOT NULL`)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 2', '2', 'retired', 'manual')`, tenant, godelOne)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Godel 1 - Part 2', 'active')`,
		tenant, activeRetiredPen, godelOne)
	exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'pen', 'Isolation 1', 'active')`,
		tenant, isolationPen, godelOne)
	exec(`INSERT INTO goats (
  goat_id, tenant_id, display_id, sex, lifecycle_status, custodian_party_id,
  current_location_id, park_id, shed_id
) VALUES
  ($1::uuid, $2::uuid, 'G-900001', 'female', 'alive', $3::uuid, $4::uuid, $6::uuid, $4::uuid),
  ($5::uuid, $2::uuid, 'G-900002', 'female', 'alive', $3::uuid, $7::uuid, $6::uuid, $7::uuid)`,
		partitionedGoat, tenant, custodian, godelOne, undividedGoat, park, castroOne)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 1', 'Godel 1 - Part 1')`,
		tenant, partitionedGoat, godelOne)

	raw, err := os.ReadFile("000148_partition_operational_location_mapping.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(raw))); err != nil {
		t.Fatalf("replay 000148: %v", err)
	}

	var currentLocationID, shedID, currentType, parentID string
	if err := pool.QueryRow(ctx, `
SELECT g.current_location_id::text, g.shed_id::text, cur.location_type, cur.parent_location_id::text
FROM goats g
JOIN locations cur ON cur.tenant_id=g.tenant_id AND cur.location_id=g.current_location_id
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid`, tenant, partitionedGoat).
		Scan(&currentLocationID, &shedID, &currentType, &parentID); err != nil {
		t.Fatalf("query partitioned goat: %v", err)
	}
	if currentLocationID == godelOne {
		t.Fatalf("partitioned goat current_location_id stayed on parent shed %s", godelOne)
	}
	if currentLocationID == isolationPen {
		t.Fatalf("partitioned goat current_location_id reused unrelated numbered pen %s", isolationPen)
	}
	if shedID != godelOne {
		t.Fatalf("partitioned goat shed_id=%s, want parent rollup %s", shedID, godelOne)
	}
	if currentType != "pen" || parentID != godelOne {
		t.Fatalf("partitioned goat current location type/parent = %s/%s, want pen/%s", currentType, parentID, godelOne)
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

	exec(`UPDATE goats SET lifecycle_status='sold' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenant, partitionedGoat)
	if _, err := pool.Exec(ctx, migrationDown(string(raw))); err != nil {
		t.Fatalf("rollback 000148: %v", err)
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
}

func migrationDown(sqlText string) string {
	parts := strings.SplitN(sqlText, "-- +goose Down", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
