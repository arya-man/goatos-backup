package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

func TestListQueueEmitsOperationalLocationDisplay_Partitioned_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a partitioned shed (Castro with partition "2")
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Castro', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item with shed_id and partition_label
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		ShedID:         &shedID,
		ParkID:         &parkID,
		PartitionLabel: stringPtr("2"),
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:partitioned:item",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for partitioned shed
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Castro - 2'")
			}
			if *item.OperationalLocationDisplay != "Castro - 2" {
				t.Fatalf("operational_location_display = %q, want 'Castro - 2'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func TestListQueueEmitsOperationalLocationDisplay_NonPartitioned_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a non-partitioned shed (Yashoda)
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CBE', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Yashoda', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item WITHOUT partition_label (non-partitioned)
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		ShedID:         &shedID,
		ParkID:         &parkID,
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:nonpartitioned:item",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for non-partitioned shed
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Yashoda'")
			}
			if *item.OperationalLocationDisplay != "Yashoda" {
				t.Fatalf("operational_location_display = %q, want 'Yashoda'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func TestListQueueEmitsOperationalLocationDisplay_PrefixedPartition_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a shed with prefixed partition (Godel 1 - Part 3)
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Godel 1', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item with prefixed partition label
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		ShedID:         &shedID,
		ParkID:         &parkID,
		PartitionLabel: stringPtr("Part 3"),
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:prefixed:partition",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for prefixed partition
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Godel 1 - Part 3'")
			}
			if *item.OperationalLocationDisplay != "Godel 1 - Part 3" {
				t.Fatalf("operational_location_display = %q, want 'Godel 1 - Part 3'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func stringPtr(s string) *string {
	return &s
}

func listQueueParams(tenantID, category string) ports.ListQueueParams {
	return ports.ListQueueParams{
		TenantID:        tenantID,
		Status:          "",
		Category:        category,
		Limit:           100,
		Vertical:        "",
		Module:          "",
		ScopeRestricted: false,
	}
}

// projection-review: membership=verification_items (one row per item); group_key=(shed_id,
// partition_label) taken from that same row; join_cardinality=locations joined 1:1 on
// location_id so no fan-out; the filter-option list is a whole-filter aggregate, not page-local;
// scope=(tenant, category) unchanged.
//
// TestVerificationFilterOptionsOneToManyPartitionsDoNotCollapse is the adversarial cardinality
// case: ONE physical shed name shared by TWO parks, and one of those sheds carrying TWO
// partitions. Grouping by shed NAME (the defect this convention bans) collapses all of it into a
// single option; grouping by (shed_id, partition_label) keeps them distinct. It also pins that a
// two-park name collision never merges, which is the failure the maintainer would see as one
// dropdown row where two different pens belong.
func TestVerificationFilterOptionsOneToManyPartitionsDoNotCollapse(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	mkPark := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', $2, 'active') RETURNING location_id::text`, tenantID, name).Scan(&id); err != nil {
			t.Fatalf("insert park %s: %v", name, err)
		}
		return id
	}
	mkShed := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', $2, 'active') RETURNING location_id::text`, tenantID, name).Scan(&id); err != nil {
			t.Fatalf("insert shed %s: %v", name, err)
		}
		return id
	}

	// Two parks, each with a shed literally named "Castro" -- this really exists in the farm data.
	cbe, cpt := mkPark("CBE"), mkPark("CPT")
	cbeCastro, cptCastro := mkShed("Castro"), mkShed("Castro")

	add := func(park, shed string, partition *string, key string) {
		in := domain.CreateItem{
			TenantID: tenantID,
			Vertical: "preventive_care",
			Module:   "vaccination",
			Category: "vaccination_proof",
			Source: domain.SourceRef{
				Module:  "vaccination",
				RefType: "vaccination_goat",
				RefID:   tenantID,
			},
			MediaRefs:      []string{"proof-" + key},
			ShedID:         &shed,
			ParkID:         &park,
			PartitionLabel: partition,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "cardinality:" + key,
		}
		if _, err := repo.CreateItem(ctx, in); err != nil {
			t.Fatalf("create item %s: %v", key, err)
		}
	}

	// CBE Castro is subdivided into two partitions; CPT Castro is not partitioned at all.
	add(cbe, cbeCastro, stringPtr("1"), "cbe-p1")
	add(cbe, cbeCastro, stringPtr("2"), "cbe-p2")
	add(cpt, cptCastro, nil, "cpt-whole")

	opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
	})
	if err != nil {
		t.Fatalf("filter options: %v", err)
	}

	// Name-keyed grouping yields ONE "Castro"; correct id+partition grouping yields THREE options.
	if len(opts.Sheds) != 3 {
		labels := make([]string, 0, len(opts.Sheds))
		for _, s := range opts.Sheds {
			labels = append(labels, s.Label)
		}
		t.Fatalf("expected 3 distinct shed options (CBE Castro 1, CBE Castro 2, CPT Castro), got %d: %v", len(opts.Sheds), labels)
	}

	seen := map[string]bool{}
	for _, s := range opts.Sheds {
		if seen[s.ID] {
			t.Fatalf("duplicate option id %s -- options must be uniquely keyed", s.ID)
		}
		seen[s.ID] = true
		if s.Label == "Castro whole" {
			t.Fatalf("rendered the 'whole' sentinel to a user: %q", s.Label)
		}
	}
}

// TestVerificationFilterOptionsStatusBucketsKeepPartitionsDistinct is the adversarial STATUS
// case for the same grouping. The live status matrix is pending / approved / rejected, and a
// decided item may also be closed. A shed option must survive regardless of the status of the
// items under it: filtering by status changes WHICH rows match, never whether two partitions of
// one shed are still two distinct options. This pins the bug where a decided-and-closed
// partition silently vanished from the picker and its work looked like it belonged to the parent.
func TestVerificationFilterOptionsStatusBucketsKeepPartitionsDistinct(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	var parkID, shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CBE', 'active') RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Godel 1', 'active') RETURNING location_id::text`, tenantID).Scan(&shedID); err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Same physical shed, two partitions, and every status the matrix supports.
	statuses := []struct {
		partition string
		status    string
		key       string
	}{
		{"Part 1", string(domain.StatusPending), "p1-pending"},
		{"Part 1", string(domain.StatusApproved), "p1-approved"},
		{"Part 3", string(domain.StatusRejected), "p3-rejected"},
		{"Part 3", string(domain.StatusApproved), "p3-approved"},
	}
	for _, s := range statuses {
		partition := s.partition
		item, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID,
			Vertical: "preventive_care",
			Module:   "vaccination",
			Category: "vaccination_proof",
			Source: domain.SourceRef{
				Module:  "vaccination",
				RefType: "vaccination_goat",
				RefID:   tenantID,
			},
			MediaRefs:      []string{"proof-" + s.key},
			ShedID:         &shedID,
			ParkID:         &parkID,
			PartitionLabel: &partition,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "statusmatrix:" + s.key,
		})
		if err != nil {
			t.Fatalf("create item %s: %v", s.key, err)
		}
		if s.status != string(domain.StatusPending) {
			// A rejected row must carry a reason -- verification_items_reject_reason_check
			// enforces it. Setting the status alone made this test fail the moment it was
			// actually RUN against Postgres (these tests are opt-in, so it never was).
			reason := ""
			if s.status == string(domain.StatusRejected) {
				reason = "partition bucket fixture"
			}
			if _, err := pool.Exec(ctx, `UPDATE verification_items SET status = $3, verdict_reason = nullif($4, '') WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
				tenantID, item.Item.ItemID, s.status, reason); err != nil {
				t.Fatalf("set status %s: %v", s.status, err)
			}
		}
	}

	// Whole-matrix read: both partitions must be present and named, never collapsed to "Godel 1".
	opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
	})
	if err != nil {
		t.Fatalf("filter options: %v", err)
	}
	labels := map[string]bool{}
	for _, s := range opts.Sheds {
		labels[s.Label] = true
	}
	for _, want := range []string{"Godel 1 - Part 1", "Godel 1 - Part 3"} {
		if !labels[want] {
			t.Fatalf("expected option %q across the status matrix, got %v", want, labels)
		}
	}
	if labels["Godel 1"] {
		t.Fatalf("partitions collapsed onto the parent shed label: %v", labels)
	}
}

// TestVerificationFilterOptionsPageBoundaryWholeFilterNotPageLocal is the adversarial PAGINATION
// case. Filter options are a WHOLE-FILTER aggregate: they describe every shed the filter could
// select, not merely the sheds present on the current page. Deriving them from a page of rows
// makes the picker shrink as the reader scrolls, which is the "capped read-time rollup presented
// as truth" anti-pattern. This seeds more items than any single page returns and asserts the
// option set is identical regardless of the row limit.
func TestVerificationFilterOptionsPageBoundaryWholeFilterNotPageLocal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	var parkID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active') RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	var shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Mandela 1', 'active') RETURNING location_id::text`, tenantID).Scan(&shedID); err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Five partitions, several items each, so the last partitions fall well past a small page.
	partitions := []string{"Part 1", "Part 2", "Part 3", "Part 4", "Part 5"}
	for _, p := range partitions {
		partition := p
		for i := 0; i < 3; i++ {
			if _, err := repo.CreateItem(ctx, domain.CreateItem{
				TenantID: tenantID,
				Vertical: "preventive_care",
				Module:   "vaccination",
				Category: "vaccination_proof",
				Source: domain.SourceRef{
					Module:  "vaccination",
					RefType: "vaccination_goat",
					RefID:   tenantID,
				},
				MediaRefs:      []string{"proof"},
				ShedID:         &shedID,
				ParkID:         &parkID,
				PartitionLabel: &partition,
				CapturedAt:     time.Now().In(biztime.DefaultLocation()),
				IdempotencyKey: "pagination:" + partition + ":" + string(rune('a'+i)),
			}); err != nil {
				t.Fatalf("create item %s/%d: %v", partition, i, err)
			}
		}
	}

	optionSet := func(limit int) map[string]bool {
		opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
			TenantID: tenantID,
			Category: "vaccination_proof",
			Limit:    limit,
		})
		if err != nil {
			t.Fatalf("filter options (limit=%d): %v", limit, err)
		}
		set := map[string]bool{}
		for _, s := range opts.Sheds {
			set[s.Label] = true
		}
		return set
	}

	small, large := optionSet(2), optionSet(500)
	if len(small) != len(partitions) {
		t.Fatalf("option set went page-local: limit=2 produced %d options, expected all %d partitions: %v", len(small), len(partitions), small)
	}
	for label := range large {
		if !small[label] {
			t.Fatalf("option %q present at limit=500 but missing at limit=2 -- options must be whole-filter, not page-local", label)
		}
	}
}

// TestVerificationFilterOptionsParkScopeHierarchyIsolatesTenantsAndParks is the adversarial SCOPE
// case. A scoped reader (park-restricted leadership, or a verifier limited to their parks) must
// see the sheds of THEIR parks only, and a same-named shed in another park must never leak in on
// the strength of its name. This is the scope twin of the cardinality test above.
func TestVerificationFilterOptionsParkScopeHierarchyIsolatesTenantsAndParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	mk := func(kind, name string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), $2, $3, 'active') RETURNING location_id::text`, tenantID, kind, name).Scan(&id); err != nil {
			t.Fatalf("insert %s %s: %v", kind, name, err)
		}
		return id
	}
	cbe, cpt := mk("park", "CBE"), mk("park", "CPT")
	cbeGandhi, cptGandhi := mk("shed", "Gandhi 1"), mk("shed", "Gandhi 1")

	add := func(park, shed, partition, key string) {
		p := partition
		if _, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: tenantID},
			MediaRefs:      []string{"proof"},
			ShedID:         &shed,
			ParkID:         &park,
			PartitionLabel: &p,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "scope:" + key,
		}); err != nil {
			t.Fatalf("create item %s: %v", key, err)
		}
	}
	add(cbe, cbeGandhi, "Part 1", "cbe")
	add(cpt, cptGandhi, "Part 1", "cpt")

	// Scoped to CBE only: the identically-named CPT shed must not appear.
	opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID:        tenantID,
		Category:        "vaccination_proof",
		ScopeRestricted: true,
		ParkIDs:         []string{cbe},
	})
	if err != nil {
		t.Fatalf("scoped filter options: %v", err)
	}
	if len(opts.Sheds) != 1 {
		t.Fatalf("park-scoped read returned %d shed options, expected only CBE's: %+v", len(opts.Sheds), opts.Sheds)
	}
	// Shed option IDs are oploc.Key() ("<uuid>#<normalized partition>"), not bare uuids:
	// one partitioned shed yields one option per partition and a bare uuid cannot tell them
	// apart. What this test asserts is the PARK isolation, so compare the shed half.
	gotShedID, _ := splitShedFilter(opts.Sheds[0].ID)
	if gotShedID != cbeGandhi {
		t.Fatalf("park-scoped read returned the wrong park's shed: got id %s (from option %q), want CBE's %s (same name, different park)", gotShedID, opts.Sheds[0].ID, cbeGandhi)
	}
}

// TestVerificationFilterOptionsExecutionDateWindowDoesNotDropPartitions is the adversarial DATE
// case: narrowing the captured-at window changes which ITEMS match, but a partition whose items
// all fall inside the window must still be offered, and one whose items all fall outside must not
// silently reappear under its parent shed's name.
func TestVerificationFilterOptionsExecutionDateWindowDoesNotDropPartitions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	var parkID, shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CBE', 'active') RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Sumathi 1', 'active') RETURNING location_id::text`, tenantID).Scan(&shedID); err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Business dates in IST, per the business-day rule -- never hour arithmetic.
	today := biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation()))
	yesterday := today.AddDate(0, 0, -1)

	add := func(partition string, at time.Time, key string) {
		p := partition
		if _, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: tenantID},
			MediaRefs:      []string{"proof"},
			ShedID:         &shedID,
			ParkID:         &parkID,
			PartitionLabel: &p,
			CapturedAt:     at,
			IdempotencyKey: "date:" + key,
		}); err != nil {
			t.Fatalf("create item %s: %v", key, err)
		}
	}
	add("Part 1", today, "p1-today")
	add("Part 2", yesterday, "p2-yesterday")

	opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID:     tenantID,
		Category:     "vaccination_proof",
		CapturedFrom: &today,
	})
	if err != nil {
		t.Fatalf("date-windowed filter options: %v", err)
	}
	labels := map[string]bool{}
	for _, s := range opts.Sheds {
		labels[s.Label] = true
	}
	if !labels["Sumathi 1 - Part 1"] {
		t.Fatalf("today's partition missing from a today-scoped read: %v", labels)
	}
	if labels["Sumathi 1"] {
		t.Fatalf("a partition collapsed onto the parent shed label under a date window: %v", labels)
	}
}

// END-TO-END for the composite shed filter key. The unit tests around
// splitShedFilter prove the string is decoded; this proves the decoded halves
// actually SELECT the right rows through real SQL, which is where the original
// defect lived: the filter option ID is oploc.Key() ("<uuid>#<partition>"), the
// queue casts the shed filter to ::uuid, and Postgres rejected the composite
// outright with `invalid input syntax for type uuid`. The request errored and the
// client fell back to cached rows, so the operator saw stale data and no error.
//
// Covers all four filter surfaces together, because a partition predicate applied
// to the list but not to the counts (or vice versa) is its own visible bug: the
// header would claim a number the list does not show.
func TestVerificationCompositeShedFilterKeySelectsOnePartitionEndToEnd(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	var parkID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active') RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	var shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Godel 1', 'active') RETURNING location_id::text`, tenantID).Scan(&shedID); err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	add := func(partition *string, key string) {
		in := domain.CreateItem{
			TenantID: tenantID,
			Vertical: "preventive_care",
			Module:   "vaccination",
			Category: "vaccination_proof",
			Source: domain.SourceRef{
				Module:  "vaccination",
				RefType: "vaccination_goat",
				RefID:   tenantID,
			},
			MediaRefs:      []string{"proof-" + key},
			ShedID:         &shedID,
			ParkID:         &parkID,
			PartitionLabel: partition,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "composite-filter:" + key,
		}
		if _, err := repo.CreateItem(ctx, in); err != nil {
			t.Fatalf("create item %s: %v", key, err)
		}
	}

	// One shed, three partitions -- the shape that makes a bare shed uuid useless
	// as a filter: all three rows share it.
	add(stringPtr("Part 1"), "p1")
	add(stringPtr("Part 2"), "p2a")
	add(stringPtr("Part 2"), "p2b")

	// The composite key is exactly what ListQueueFilterOptions hands the client.
	compositeP2 := shedID + "#2"

	items, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
		ShedID:   compositeP2,
		Limit:    50,
	})
	if err != nil {
		// This is the original failure mode: `invalid input syntax for type uuid`.
		t.Fatalf("list queue with composite shed key: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("composite key %q selected %d items, want the 2 in Part 2", compositeP2, len(items))
	}
	for _, it := range items {
		if it.PartitionLabel == nil || *it.PartitionLabel != "Part 2" {
			t.Fatalf("selected an item outside the filtered partition: %+v", it.PartitionLabel)
		}
	}

	// A bare uuid must keep working and match EVERY partition -- an older client
	// still sends this shape, and silently narrowing it would hide rows.
	all, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
		ShedID:   shedID,
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("list queue with bare shed uuid: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("bare shed uuid selected %d items, want all 3 partitions", len(all))
	}

	// The counts must agree with the list they describe.
	opts, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
		ShedID:   compositeP2,
	})
	if err != nil {
		t.Fatalf("filter options with composite shed key: %v", err)
	}
	counted := opts.Counts.Pending + opts.Counts.Approved + opts.Counts.Rejected
	if counted != len(items) {
		t.Fatalf("status counts total %d but the filtered list has %d rows -- the header would claim a number the list does not show", counted, len(items))
	}

	// Drive-closure runs the same shed filter; a composite key must not error there
	// either. Closure readiness itself is asserted elsewhere -- what matters here is
	// that the composite key survives the ::uuid cast on this path too.
	if _, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
		ShedID:   compositeP2,
	}); err != nil {
		t.Fatalf("drive closures with composite shed key: %v", err)
	}
}
