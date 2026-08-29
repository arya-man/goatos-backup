package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// A pen that ALSO exists as a legacy location row must not raise its own transport task.
//
// This is the second half of the 000152/000155 shed-grain repair. That one stopped the
// materializer fanning out over `shed_partitions`, so `partition_label` is empty on every new row --
// and the grain still came out wrong, because the farm's pens exist a SECOND time in `locations`:
// active rows typed 'shed' and named "Shed A 1" or "Shed A - Part 3", sitting beside the canonical
// parent shed plus its catalog entry. Membership written as `location_type='shed' AND
// status='active'` counts each pen again as its own building. On the live data that was 126 sheds
// against 21 real ones, and Mandela 1 alone raised eleven tasks for one load.
//
// Both alias spellings are asserted on purpose. The suppression copy in
// counts/shifting_destinations.go catches only the numeric-suffix shape: it strips
// non-alphanumerics and compares the remainder to normalized_label, so "Shed A - Part 3" leaves
// "part3" against a key of "3" and survives. Transport uses the shared oploc predicate, which
// strips a leading "- Part", so a regression in either spelling turns this red.
func TestFeedTransportSkipsLegacyPartitionAliasShedRows(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())

	// Shed A is genuinely partitioned: the catalog owns pens "1" and "Part 3".
	//
	// Seeded with the CATALOG's own normalization from migration 000112
	// (`regexp_replace(lower(btrim(label)), '^part[[:space:]]+', '')`, so "Part 3" -> "3") rather
	// than through seedFeedDirectionPartition. That helper uses feeddirection's PartitionMatchKey,
	// which yields "part3" -- a different convention from the one every shed_partitions row in the
	// live database actually carries. Seeding a shape the farm does not have is a defect even when
	// the assertion passes (AGENTS.md rule 1 from 2026-08-07), and here it would have hidden the
	// "- Part N" half of this test entirely.
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1',      '1', 'active', 'manual'),
       ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual')
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING`, fdTenant, fdShedA); err != nil {
		t.Fatalf("seed catalog partitions: %v", err)
	}

	// The same two pens, as the OLD location rows the farm still carries. Nothing on these rows
	// marks them as aliases; that is exactly why the predicate has to derive it from the name.
	const aliasNumeric = "22222222-2222-4222-8222-000000000901"
	const aliasPartWord = "22222222-2222-4222-8222-000000000902"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-A-1',  'Shed A 1',      'active', $4::uuid, 8),
       ($3::uuid, $1::uuid, 'shed', 'S-A-P3', 'Shed A - Part 3','active', $4::uuid, 9)`,
		fdTenant, aliasNumeric, aliasPartWord, fdPark); err != nil {
		t.Fatalf("seed alias locations: %v", err)
	}

	// A same-named shed in the OTHER park with no such parent must survive: the sibling check is
	// park-local because two parks really do both own a "Castro".
	const otherParkShed = "22222222-2222-4222-8222-000000000903"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-A-CBE', 'Shed A 1', 'active', $3::uuid, 1)`,
		fdTenant, otherParkShed, fdOtherPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}

	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{
		TenantID: fdTenant, AsOf: day.Add(15*time.Hour + 30*time.Minute),
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	var aliasTasks int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_transport_tasks
WHERE tenant_id = $1::uuid AND business_date = $2::date AND shed_id = ANY($3::uuid[])`,
		fdTenant, day.Format("2006-01-02"), []string{aliasNumeric, aliasPartWord}).Scan(&aliasTasks); err != nil {
		t.Fatalf("count alias tasks: %v", err)
	}
	if aliasTasks != 0 {
		t.Fatalf("legacy partition-alias locations raised %d transport tasks, want 0 -- a pen is not a shed, and its feed leaves on the shed's one trip", aliasTasks)
	}

	var shedATasks int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_transport_tasks
WHERE tenant_id = $1::uuid AND business_date = $2::date AND shed_id = $3::uuid`,
		fdTenant, day.Format("2006-01-02"), fdShedA).Scan(&shedATasks); err != nil {
		t.Fatalf("count shed A tasks: %v", err)
	}
	if shedATasks != 1 {
		t.Fatalf("the real partitioned shed has %d tasks, want exactly 1 -- suppressing aliases must not suppress the shed itself", shedATasks)
	}

	// The genuinely-distinct shed in the other park keeps its task; over-matching here would
	// silently stop feeding a real building.
	var otherParkTasks int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_transport_tasks
WHERE tenant_id = $1::uuid AND business_date = $2::date AND shed_id = $3::uuid`,
		fdTenant, day.Format("2006-01-02"), otherParkShed).Scan(&otherParkTasks); err != nil {
		t.Fatalf("count other-park tasks: %v", err)
	}
	if otherParkTasks != 1 {
		t.Fatalf("the same-named shed in the other park has %d tasks, want 1 -- the sibling check must stay park-local", otherParkTasks)
	}
}
