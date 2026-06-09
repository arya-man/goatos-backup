package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/reporting/domain"
	"github.com/vgoats/goatos/backend/internal/reporting/ports"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	secondTenant         = "00000000-0000-4000-8000-000000000002"
	meshaParty           = "00000000-0000-4000-8000-000000001001"
	boerBreed            = "00000000-0000-4000-8000-000000002005"
	cbePark              = "00000000-0000-4000-8000-000000003001"
	cptPark              = "00000000-0000-4000-8000-000000003002"
	cbeShed              = "00000000-0000-4000-8000-000000003101"
	cptShed              = "00000000-0000-4000-8000-000000003102"
	t2Park               = "00000000-0000-4000-8000-000000003201"
)

func TestIdentityCounterRebuildWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startReportingDB(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	seedReportingData(t, pool)
	runID := seedImportRun(t, pool, meshaTenant)

	t.Run("analytics reads projection rows only", func(t *testing.T) {
		page, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: meshaTenant,
			Grain:    domain.GrainTenantLifecycle,
			Limit:    100,
		})
		if err != nil {
			t.Fatalf("ListIdentityCounts before rebuild: %v", err)
		}
		if len(page.Items) != 0 || page.Freshness.AsOfRecordedAt != nil {
			t.Fatalf("counts should be empty before rebuild, got page=%#v", page)
		}
	})

	result, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{
		TenantID:          meshaTenant,
		Grains:            domain.AllIdentityCounterGrains,
		SourceImportRunID: &runID,
	})
	if err != nil {
		t.Fatalf("RebuildIdentityCounters: %v", err)
	}
	if len(result.Grains) != len(domain.AllIdentityCounterGrains) {
		t.Fatalf("rebuilt grains=%d want %d", len(result.Grains), len(domain.AllIdentityCounterGrains))
	}
	wantWatermark := time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC)
	if result.AsOfRecordedAt == nil || !result.AsOfRecordedAt.Equal(wantWatermark) {
		t.Fatalf("watermark=%v want %v", result.AsOfRecordedAt, wantWatermark)
	}
	if _, err := repo.ListIdentityCounts(ctx, ports.CountParams{
		TenantID: meshaTenant,
		Grain:    domain.GrainParkLifecycle,
		Limit:    100,
		ParkID:   strPtr("not-a-uuid"),
	}); !errors.Is(err, ports.ErrInvalidFilter) {
		t.Fatalf("malformed park_id should fail closed with ErrInvalidFilter, got %v", err)
	}

	assertCounter(t, pool, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)
	assertCounter(t, pool, domain.GrainTenantLifecycle, `lifecycle_status = 'dead'`, 1)
	assertCounter(t, pool, domain.GrainTenantLifecycle, `lifecycle_status = 'sold'`, 1)
	assertCounter(t, pool, domain.GrainCustodianLife, `custodian_party_id = '`+meshaParty+`'::uuid AND lifecycle_status = 'sold'`, 1)
	assertCounter(t, pool, domain.GrainCustodianIdent, `custodian_party_id = '`+meshaParty+`'::uuid AND identity_state = 'clean'`, 1)
	assertCounter(t, pool, domain.GrainCustodianIdent, `custodian_party_id = '`+meshaParty+`'::uuid AND identity_state = 'disputed'`, 1)
	assertCounter(t, pool, domain.GrainParkLifecycle, `park_id = '`+cbePark+`'::uuid AND lifecycle_status = 'alive'`, 1)
	assertCounter(t, pool, domain.GrainParkLifecycle, `park_id IS NULL AND lifecycle_status = 'alive'`, 1)
	assertCounter(t, pool, domain.GrainShedLifecycle, `park_id = '`+cbePark+`'::uuid AND shed_id = '`+cbeShed+`'::uuid AND lifecycle_status = 'dead'`, 1)
	assertCounter(t, pool, domain.GrainBreedSexLife, `breed_id IS NULL AND sex IS NULL AND lifecycle_status = 'alive'`, 1)
	assertCounter(t, pool, domain.GrainHealthStatus, `health_status = 'healthy'`, 1)
	assertCounter(t, pool, domain.GrainHealthStatus, `health_status IS NULL`, 1)
	assertCounter(t, pool, domain.GrainGrowthCohort, `growth_cohort_tag = 'F2'`, 1)
	assertCounter(t, pool, domain.GrainManagementStage, `management_stage = 'warmup'`, 1)
	assertCounter(t, pool, domain.GrainReproductiveStat, `reproductive_status = 'non_pregnant'`, 1)
	assertNoCounter(t, pool, domain.GrainHealthStatus, `health_status = 'icu'`)
	assertNoCounter(t, pool, "farm_lifecycle", `true`)
	assertNoCounter(t, pool, "cohort_lifecycle", `true`)
	assertAllCountersStamped(t, pool, runID, wantWatermark)

	t.Run("idempotent rerun deletes vanished buckets and does not touch other tenant", func(t *testing.T) {
		seedOtherTenantCounter(t, pool)
		if _, err := pool.Exec(ctx, `
UPDATE goats
SET identity_state = 'inactive'
WHERE tenant_id = $1 AND goat_id = '10000000-0000-4000-8000-000000000004'`, meshaTenant); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{TenantID: meshaTenant, Grains: domain.AllIdentityCounterGrains}); err != nil {
			t.Fatalf("second rebuild: %v", err)
		}
		assertNoCounter(t, pool, domain.GrainTenantLifecycle, `lifecycle_status = 'sold'`)
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_counters WHERE tenant_id = $1 AND counter_grain = 'tenant_lifecycle'`, meshaTenant); got != 2 {
			t.Fatalf("tenant lifecycle rows after rerun=%d, want 2", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_counters WHERE tenant_id = $1`, secondTenant); got != 1 {
			t.Fatalf("other tenant counters touched, got %d", got)
		}
	})

	t.Run("empty grain returns empty items and null event watermark stays null", func(t *testing.T) {
		if _, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{
			TenantID: secondTenant,
			Grains:   []string{domain.GrainHealthStatus},
		}); err != nil {
			t.Fatalf("tenant without events rebuild: %v", err)
		}
		allPage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: secondTenant,
			Grain:    domain.GrainHealthStatus,
			Limit:    100,
		})
		if err != nil {
			t.Fatalf("ListIdentityCounts no-event tenant: %v", err)
		}
		if len(allPage.Items) != 1 || allPage.Items[0].AsOfRecordedAt != nil || allPage.Freshness.AsOfRecordedAt != nil {
			t.Fatalf("no-event watermark should be null, got page=%#v", allPage)
		}
		page, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID:     secondTenant,
			Grain:        domain.GrainHealthStatus,
			Limit:        100,
			HealthStatus: strPtr("icu"),
		})
		if err != nil {
			t.Fatalf("ListIdentityCounts empty filter: %v", err)
		}
		if len(page.Items) != 0 || page.Freshness.AsOfRecordedAt != nil {
			t.Fatalf("empty grain filter returned page=%#v", page)
		}
	})

	t.Run("identity counts keyset pagination is deterministic", func(t *testing.T) {
		seedPaginatedGrowthCounters(t, pool)
		firstPage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: meshaTenant,
			Grain:    domain.GrainGrowthCohort,
			Limit:    3,
		})
		if err != nil {
			t.Fatalf("first page: %v", err)
		}
		assertGrowthTags(t, firstPage.Items, []string{"page-A", "page-B", "page-C"})
		if !firstPage.HasMore || firstPage.NextCursor == nil {
			t.Fatalf("first page should have next cursor: %#v", firstPage)
		}

		secondPage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: meshaTenant,
			Grain:    domain.GrainGrowthCohort,
			Limit:    3,
			Cursor:   firstPage.NextCursor,
		})
		if err != nil {
			t.Fatalf("second page: %v", err)
		}
		assertGrowthTags(t, secondPage.Items, []string{"page-D", "page-E"})
		if secondPage.HasMore || secondPage.NextCursor != nil {
			t.Fatalf("second page should be terminal: %#v", secondPage)
		}

		seen := map[string]bool{}
		for _, item := range append(firstPage.Items, secondPage.Items...) {
			tag := ""
			if item.Dimensions.GrowthCohortTag != nil {
				tag = *item.Dimensions.GrowthCohortTag
			}
			if seen[tag] {
				t.Fatalf("duplicate paginated tag %s", tag)
			}
			seen[tag] = true
		}
		for _, tag := range []string{"page-A", "page-B", "page-C", "page-D", "page-E"} {
			if !seen[tag] {
				t.Fatalf("paginated results skipped %s; seen=%#v", tag, seen)
			}
		}

		filteredPage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID:        meshaTenant,
			Grain:           domain.GrainGrowthCohort,
			Limit:           10,
			GrowthCohortTag: strPtr("page-D"),
		})
		if err != nil {
			t.Fatalf("filtered page: %v", err)
		}
		assertGrowthTags(t, filteredPage.Items, []string{"page-D"})

		cursor, err := encodeSyntheticCountCursor(999, "90000000-0000-4000-8000-000000000999")
		if err != nil {
			t.Fatal(err)
		}
		arbitraryCursorPage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: meshaTenant,
			Grain:    domain.GrainGrowthCohort,
			Limit:    3,
			Cursor:   &cursor,
		})
		if err != nil {
			t.Fatalf("structurally valid arbitrary cursor should be clean: %v", err)
		}
		if len(arbitraryCursorPage.Items) == 0 {
			t.Fatalf("arbitrary high cursor should return clean page, got empty")
		}

		if _, err := repo.ListIdentityCounts(ctx, ports.CountParams{
			TenantID: meshaTenant,
			Grain:    domain.GrainGrowthCohort,
			Limit:    3,
			Cursor:   strPtr("not-base64url"),
		}); !errors.Is(err, ports.ErrInvalidCursor) {
			t.Fatalf("malformed cursor should fail closed with ErrInvalidCursor, got %v", err)
		}
	})

	t.Run("same tenant concurrent rebuilds serialize through advisory lock", func(t *testing.T) {
		var inHook int32
		block := make(chan struct{})
		release := make(chan struct{})
		repo.afterLock = func(context.Context) error {
			if atomic.AddInt32(&inHook, 1) == 1 {
				close(block)
				<-release
			}
			return nil
		}
		defer func() { repo.afterLock = nil }()

		errs := make(chan error, 2)
		go func() {
			_, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{TenantID: meshaTenant, Grains: []string{domain.GrainTenantLifecycle}})
			errs <- err
		}()
		<-block
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{TenantID: meshaTenant, Grains: []string{domain.GrainTenantLifecycle}})
			errs <- err
		}()
		time.Sleep(100 * time.Millisecond)
		if atomic.LoadInt32(&inHook) != 1 {
			t.Fatalf("second rebuild entered lock hook before first transaction released")
		}
		close(release)
		wg.Wait()
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("concurrent rebuild error: %v", err)
			}
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_counters WHERE tenant_id = $1 AND counter_grain = 'tenant_lifecycle'`, meshaTenant); got != 2 {
			t.Fatalf("tenant_lifecycle rows after concurrent rebuilds=%d, want 2", got)
		}
	})
}

func TestIdentityCounterIncrementalWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startReportingDB(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	seedIncrementalReportingBase(t, pool)

	firstGoat := "11000000-0000-4000-8000-000000000001"
	firstEvent := "61000000-0000-4000-8000-000000000001"
	firstRecorded := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	insertIncrementalGoat(t, pool, firstGoat, meshaTenant, cbePark, cbeShed, "alive", "clean", "healthy")
	insertIncrementalEvent(t, pool, meshaTenant, firstGoat, firstEvent, "goat.created", firstRecorded)

	rebuild, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{
		TenantID: meshaTenant,
		Grains:   domain.AllIdentityCounterGrains,
	})
	if err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if rebuild.AsOfRecordedAt == nil || !rebuild.AsOfRecordedAt.Equal(firstRecorded) {
		t.Fatalf("rebuild checkpoint=%v want %v", rebuild.AsOfRecordedAt, firstRecorded)
	}
	assertProjectionState(t, pool, meshaTenant, firstRecorded, firstEvent, false, "")

	secondGoat := "11000000-0000-4000-8000-000000000002"
	secondEvent := "61000000-0000-4000-8000-000000000002"
	secondRecorded := firstRecorded.Add(10 * time.Minute)
	insertIncrementalGoat(t, pool, secondGoat, meshaTenant, cbePark, cbeShed, "alive", "clean", "healthy")
	insertIncrementalEvent(t, pool, meshaTenant, secondGoat, secondEvent, "goat.created", secondRecorded)

	result, err := repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("incremental created goat: %v", err)
	}
	if result.ScannedEventCount != 1 || result.AppliedEventCount != 1 || result.RebuildRequired {
		t.Fatalf("unexpected created result: %#v", result)
	}
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainHealthStatus, `health_status = 'healthy'`, 2)
	assertProjectionState(t, pool, meshaTenant, secondRecorded, secondEvent, false, "")
	assertProcessedEvent(t, pool, meshaTenant, secondEvent, secondRecorded, "applied")

	page, err := repo.ListIdentityCounts(ctx, ports.CountParams{
		TenantID: meshaTenant,
		Grain:    domain.GrainTenantLifecycle,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("ListIdentityCounts after incremental: %v", err)
	}
	if page.Freshness.AsOfRecordedAt == nil || !page.Freshness.AsOfRecordedAt.Equal(secondRecorded) {
		t.Fatalf("freshness=%#v want checkpoint %v", page.Freshness, secondRecorded)
	}

	if _, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{
		TenantID: meshaTenant,
		Grains:   domain.AllIdentityCounterGrains,
	}); err != nil {
		t.Fatalf("rebuild after incremental: %v", err)
	}
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainHealthStatus, `health_status = 'healthy'`, 2)

	rewindProjectionState(t, pool, meshaTenant, firstRecorded, firstEvent)
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("duplicate processed event update: %v", err)
	}
	if result.ScannedEventCount != 1 || result.SkippedEventCount != 1 || result.AppliedEventCount != 0 {
		t.Fatalf("duplicate event should skip without reapplying: %#v", result)
	}
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)

	noopEvents := []struct {
		id        string
		eventType string
		recorded  time.Time
	}{
		{"61000000-0000-4000-8000-000000000003", "goat.identifier.added", secondRecorded.Add(1 * time.Minute)},
		{"61000000-0000-4000-8000-000000000004", "goat.identifier.retired", secondRecorded.Add(2 * time.Minute)},
		{"61000000-0000-4000-8000-000000000005", "goat.identifier.disputed", secondRecorded.Add(3 * time.Minute)},
	}
	for _, event := range noopEvents {
		insertIncrementalEvent(t, pool, meshaTenant, secondGoat, event.id, event.eventType, event.recorded)
	}
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("identifier noop events: %v", err)
	}
	if result.NoopEventCount != 3 || result.AppliedEventCount != 0 || result.RebuildRequired {
		t.Fatalf("identifier events should be noop: %#v", result)
	}
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)
	assertProjectionState(t, pool, meshaTenant, noopEvents[2].recorded, noopEvents[2].id, false, "")

	// FK must include recorded_at; same tenant/event_id with the wrong recorded_at is invalid.
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_counter_processed_events (
  tenant_id, event_id, event_recorded_at, event_type, outcome
) VALUES ($1, $2, $3, 'goat.created', 'noop')`, meshaTenant, secondEvent, secondRecorded.Add(time.Second)); err == nil {
		t.Fatal("processed event with wrong recorded_at should fail FK")
	}

	if _, err := pool.Exec(ctx, `
UPDATE goat_identity_counter_processed_events
SET processed_at = $1
WHERE tenant_id = $2 AND event_id = $3`, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), meshaTenant, secondEvent); err != nil {
		t.Fatal(err)
	}
	pruneEvent := "61000000-0000-4000-8000-000000000006"
	pruneRecorded := noopEvents[2].recorded.Add(time.Minute)
	insertIncrementalEvent(t, pool, meshaTenant, secondGoat, pruneEvent, "goat.identifier.added", pruneRecorded)
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: time.Hour,
	})
	if err != nil {
		t.Fatalf("retention prune update: %v", err)
	}
	if result.PrunedProcessedRows == 0 {
		t.Fatalf("expected processed-event pruning, got %#v", result)
	}

	mergeEvent := "61000000-0000-4000-8000-000000000007"
	mergeRecorded := pruneRecorded.Add(time.Minute)
	afterMergeGoat := "11000000-0000-4000-8000-000000000003"
	afterMergeEvent := "61000000-0000-4000-8000-000000000008"
	insertIncrementalEvent(t, pool, meshaTenant, secondGoat, mergeEvent, "goat.identity.merge_approved", mergeRecorded)
	insertIncrementalGoat(t, pool, afterMergeGoat, meshaTenant, cbePark, cbeShed, "alive", "clean", "healthy")
	insertIncrementalEvent(t, pool, meshaTenant, afterMergeGoat, afterMergeEvent, "goat.created", mergeRecorded.Add(time.Minute))
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("merge-required update: %v", err)
	}
	if !result.RebuildRequired || result.RebuildReason == nil || *result.RebuildReason != "merge_event_requires_rebuild" || result.ScannedEventCount != 1 {
		t.Fatalf("merge should require rebuild and stop: %#v", result)
	}
	assertProjectionState(t, pool, meshaTenant, pruneRecorded, pruneEvent, true, "merge_event_requires_rebuild")
	assertCounterForTenant(t, pool, meshaTenant, domain.GrainTenantLifecycle, `lifecycle_status = 'alive'`, 2)
	if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_counter_processed_events WHERE tenant_id = $1 AND event_id = $2`, meshaTenant, afterMergeEvent); got != 0 {
		t.Fatalf("event after merge should not process, processed rows=%d", got)
	}
	stalePage, err := repo.ListIdentityCounts(ctx, ports.CountParams{
		TenantID: meshaTenant,
		Grain:    domain.GrainTenantLifecycle,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("ListIdentityCounts stale: %v", err)
	}
	if stalePage.Freshness.Warning == nil || *stalePage.Freshness.Warning != "rebuild_required" {
		t.Fatalf("expected rebuild_required freshness warning, got %#v", stalePage.Freshness)
	}

	if _, err := repo.RebuildIdentityCounters(ctx, ports.RebuildIdentityCountersParams{
		TenantID: meshaTenant,
		Grains:   domain.AllIdentityCounterGrains,
	}); err != nil {
		t.Fatalf("rebuild clears required: %v", err)
	}
	unknownEvent := "61000000-0000-4000-8000-000000000009"
	unknownRecorded := mergeRecorded.Add(2 * time.Minute)
	insertIncrementalEvent(t, pool, meshaTenant, afterMergeGoat, unknownEvent, "goat.future.changed", unknownRecorded)
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 meshaTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unknown-required update: %v", err)
	}
	if !result.RebuildRequired || result.RebuildReason == nil || *result.RebuildReason != "unknown_event_requires_rebuild" {
		t.Fatalf("unknown event should require rebuild: %#v", result)
	}

	// A tenant with events but no projection state must not infer a checkpoint.
	tenantTwoGoat := "11000000-0000-4000-8000-000000000201"
	tenantTwoEventID := "61000000-0000-4000-8000-000000000201"
	insertIncrementalGoat(t, pool, tenantTwoGoat, secondTenant, t2Park, "", "alive", "clean", "healthy")
	insertIncrementalEvent(t, pool, secondTenant, tenantTwoGoat, tenantTwoEventID, "goat.created", firstRecorded)
	result, err = repo.UpdateIdentityCounters(ctx, ports.UpdateIdentityCountersParams{
		TenantID:                 secondTenant,
		Limit:                    10,
		ProcessedEventsRetention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("missing second tenant state update: %v", err)
	}
	if !result.RebuildRequired || result.RebuildReason == nil || *result.RebuildReason != "missing_projection_checkpoint" {
		t.Fatalf("missing state with events should require rebuild: %#v", result)
	}
	if got := countRows(t, pool, `SELECT count(*) FROM goat_identity_counters WHERE tenant_id = $1`, secondTenant); got != 0 {
		t.Fatalf("second tenant should not get counters without rebuild, got %d", got)
	}
}

func startReportingDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container := fmt.Sprintf("goatos-reporting-test-%d", time.Now().UnixNano())
	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", image)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			applyMigrations(t, container)
			return openPool(t, ctx, container)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
	return nil
}

func seedReportingData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1, 'Synthetic second tenant', 'active')
ON CONFLICT DO NOTHING;
`, secondTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($1, $3, 'shed', 'CBE-S1', 'Synthetic CBE shed', $4, 'active'),
  ($2, $3, 'shed', 'CPT-S1', 'Synthetic CPT shed', $5, 'active')
ON CONFLICT DO NOTHING;
`, cbeShed, cptShed, meshaTenant, cbePark, cptPark); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1, $2, 'park', 'T2P', 'Synthetic tenant 2 park', 'active')
ON CONFLICT DO NOTHING;
`, t2Park, secondTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, species, breed, breed_id, sex, lifecycle_status,
  reproductive_status, growth_cohort_tag, management_stage, health_status,
  identity_state, custodian_party_id, current_location_id, park_id, shed_id,
  merged_into_goat_id
) VALUES
  ('10000000-0000-4000-8000-000000000001', $1, 'goat', 'Boer', $2, 'female', 'alive', 'non_pregnant', 'F2', 'warmup', 'healthy', 'clean', $3, $4, $4, $5, NULL),
  ('10000000-0000-4000-8000-000000000002', $1, 'goat', NULL, NULL, NULL, 'alive', NULL, NULL, NULL, NULL, 'disputed', $3, NULL, NULL, NULL, NULL),
  ('10000000-0000-4000-8000-000000000003', $1, 'goat', 'Boer', $2, 'male', 'dead', 'buck', 'F2', 'warmup', 'icu', 'clean', $3, $4, $4, $5, NULL),
  ('10000000-0000-4000-8000-000000000004', $1, 'goat', 'Boer', $2, 'female', 'sold', 'mother', 'F2', 'warmup', 'healthy', 'clean', $3, $6, $6, $7, NULL),
  ('10000000-0000-4000-8000-000000000005', $1, 'goat', 'Boer', $2, 'female', 'alive', 'non_pregnant', 'F2', 'warmup', 'healthy', 'merged', $3, $4, $4, $5, '10000000-0000-4000-8000-000000000001'),
  ('10000000-0000-4000-8000-000000000006', $1, 'goat', 'Boer', $2, 'female', 'alive', 'non_pregnant', 'F2', 'warmup', 'healthy', 'inactive', $3, $4, $4, $5, NULL),
  ('10000000-0000-4000-8000-000000000101', $8, 'goat', 'Boer', $2, 'female', 'alive', 'non_pregnant', 'F2', 'warmup', 'healthy', 'clean', $3, $9, $9, NULL, NULL);
`, meshaTenant, boerBreed, meshaParty, cbePark, cbeShed, cptPark, cptShed, secondTenant, t2Park); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_events (tenant_id, goat_id, event_type, event_version, occurred_at, recorded_at, payload, idempotency_key)
VALUES
  ($1, '10000000-0000-4000-8000-000000000001', 'goat.created', 1, '2026-06-09 10:00:00+00', '2026-06-09 10:00:00+00', '{}'::jsonb, 'synthetic-reporting-event-1'),
  ($1, '10000000-0000-4000-8000-000000000002', 'goat.identifier.disputed', 1, '2026-06-09 11:00:00+00', '2026-06-09 12:30:00+00', '{}'::jsonb, 'synthetic-reporting-event-2');
`, meshaTenant); err != nil {
		t.Fatal(err)
	}
}

func seedIncrementalReportingBase(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1, 'Synthetic second tenant', 'active')
ON CONFLICT DO NOTHING;
`, secondTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, 'shed', 'CBE-S1', 'Synthetic CBE shed', $3, 'active')
ON CONFLICT DO NOTHING;
`, cbeShed, meshaTenant, cbePark); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1, $2, 'park', 'T2P', 'Synthetic tenant 2 park', 'active')
ON CONFLICT DO NOTHING;
`, t2Park, secondTenant); err != nil {
		t.Fatal(err)
	}
}

func insertIncrementalGoat(t *testing.T, pool *pgxpool.Pool, goatID string, tenantID string, parkID string, shedID string, lifecycleStatus string, identityState string, healthStatus string) {
	t.Helper()
	var shed any
	if shedID != "" {
		shed = shedID
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goats (
  goat_id, tenant_id, species, breed, breed_id, sex, lifecycle_status,
  reproductive_status, growth_cohort_tag, management_stage, health_status,
  identity_state, custodian_party_id, current_location_id, park_id, shed_id
) VALUES (
  $1, $2, 'goat', 'Boer', $3, 'female', $4,
  'non_pregnant', 'F2', 'warmup', $5,
  $6, $7, $8, $8, $9
)`, goatID, tenantID, boerBreed, lifecycleStatus, healthStatus, identityState, meshaParty, parkID, shed); err != nil {
		t.Fatal(err)
	}
}

func insertIncrementalEvent(t *testing.T, pool *pgxpool.Pool, tenantID string, goatID string, eventID string, eventType string, recordedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, payload, idempotency_key
) VALUES (
  $1, $2, $3, $4, 1,
  $5, $5, '{}'::jsonb, $6
)`, eventID, tenantID, goatID, eventType, recordedAt, "synthetic-reporting-incremental-"+eventID); err != nil {
		t.Fatal(err)
	}
}

func seedImportRun(t *testing.T, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	runID := "30000000-0000-4000-8000-000000000701"
	if _, err := pool.Exec(context.Background(), `
INSERT INTO legacy_import_runs (
  import_run_id, tenant_id, source_name, source_system, source_dataset,
  source_file_hash, policy_version, dry_run, status, row_count,
  created_goat_count, updated_goat_count, conflict_count, error_count
) VALUES (
  $1, $2, 'Synthetic reporting source', 'legacy_rfid_db', 'rfid_db_first_import',
  'sha256:reporting-synthetic', 'phase1-rfid-db-import-v1', false, 'completed', 0,
  0, 0, 0, 0
) ON CONFLICT DO NOTHING`, runID, tenantID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func seedOtherTenantCounter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_identity_counters (counter_grain, tenant_id, lifecycle_status, count_value, is_rebuilding)
VALUES ('tenant_lifecycle', $1, 'alive', 99, false)
ON CONFLICT DO NOTHING`, secondTenant); err != nil {
		t.Fatal(err)
	}
}

func seedPaginatedGrowthCounters(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
DELETE FROM goat_identity_counters
WHERE tenant_id = $1 AND counter_grain = 'growth_cohort';
`, meshaTenant); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_counters (
  counter_id, counter_grain, tenant_id, growth_cohort_tag, count_value,
  as_of_recorded_at, is_rebuilding, updated_at
) VALUES
  ('90000000-0000-4000-8000-000000000001', 'growth_cohort', $1, 'page-A', 50, '2026-06-09 12:30:00+00', false, '2026-06-09 12:31:00+00'),
  ('90000000-0000-4000-8000-000000000002', 'growth_cohort', $1, 'page-B', 50, '2026-06-09 12:30:00+00', false, '2026-06-09 12:31:00+00'),
  ('90000000-0000-4000-8000-000000000003', 'growth_cohort', $1, 'page-C', 40, '2026-06-09 12:30:00+00', false, '2026-06-09 12:31:00+00'),
  ('90000000-0000-4000-8000-000000000004', 'growth_cohort', $1, 'page-D', 30, '2026-06-09 12:30:00+00', false, '2026-06-09 12:31:00+00'),
  ('90000000-0000-4000-8000-000000000005', 'growth_cohort', $1, 'page-E', 30, '2026-06-09 12:30:00+00', false, '2026-06-09 12:31:00+00');
`, meshaTenant); err != nil {
		t.Fatal(err)
	}
}

func assertGrowthTags(t *testing.T, items []domain.IdentityCount, want []string) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("items=%d want %d: %#v", len(items), len(want), items)
	}
	for i, item := range items {
		if item.Dimensions.GrowthCohortTag == nil || *item.Dimensions.GrowthCohortTag != want[i] {
			t.Fatalf("item[%d] tag=%v want %s in %#v", i, item.Dimensions.GrowthCohortTag, want[i], items)
		}
	}
}

func encodeSyntheticCountCursor(countValue int64, counterID string) (string, error) {
	version := identityCountsCursorVersion
	payload, err := json.Marshal(countCursorPayload{
		Version:    &version,
		CountValue: &countValue,
		CounterID:  &counterID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func assertCounter(t *testing.T, pool *pgxpool.Pool, grain, predicate string, want int64) {
	t.Helper()
	assertCounterForTenant(t, pool, meshaTenant, grain, predicate, want)
}

func assertCounterForTenant(t *testing.T, pool *pgxpool.Pool, tenantID, grain, predicate string, want int64) {
	t.Helper()
	var got int64
	query := fmt.Sprintf(`SELECT COALESCE(sum(count_value), 0)::bigint FROM goat_identity_counters WHERE tenant_id = $1 AND counter_grain = $2 AND %s`, predicate)
	if err := pool.QueryRow(context.Background(), query, tenantID, grain).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s %s count=%d want %d", grain, predicate, got, want)
	}
}

func assertProjectionState(t *testing.T, pool *pgxpool.Pool, tenantID string, wantRecordedAt time.Time, wantEventID string, wantRebuildRequired bool, wantReason string) {
	t.Helper()
	var gotRecordedAt time.Time
	var gotEventID string
	var gotRebuildRequired bool
	var gotReason string
	if err := pool.QueryRow(context.Background(), `
SELECT last_processed_recorded_at, last_processed_event_id::text, rebuild_required, COALESCE(rebuild_reason, '')
FROM goat_identity_counter_projection_state
WHERE tenant_id = $1`, tenantID).Scan(&gotRecordedAt, &gotEventID, &gotRebuildRequired, &gotReason); err != nil {
		t.Fatal(err)
	}
	if !gotRecordedAt.Equal(wantRecordedAt) || gotEventID != wantEventID || gotRebuildRequired != wantRebuildRequired || gotReason != wantReason {
		t.Fatalf("projection state=(%s,%s,%t,%q), want (%s,%s,%t,%q)", gotRecordedAt, gotEventID, gotRebuildRequired, gotReason, wantRecordedAt, wantEventID, wantRebuildRequired, wantReason)
	}
}

func assertProcessedEvent(t *testing.T, pool *pgxpool.Pool, tenantID, eventID string, recordedAt time.Time, wantOutcome string) {
	t.Helper()
	var gotOutcome string
	if err := pool.QueryRow(context.Background(), `
SELECT outcome
FROM goat_identity_counter_processed_events
WHERE tenant_id = $1 AND event_id = $2 AND event_recorded_at = $3`, tenantID, eventID, recordedAt).Scan(&gotOutcome); err != nil {
		t.Fatal(err)
	}
	if gotOutcome != wantOutcome {
		t.Fatalf("processed event %s outcome=%s want %s", eventID, gotOutcome, wantOutcome)
	}
}

func rewindProjectionState(t *testing.T, pool *pgxpool.Pool, tenantID string, recordedAt time.Time, eventID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
UPDATE goat_identity_counter_projection_state
SET last_processed_recorded_at = $2,
    last_processed_event_id = $3,
    rebuild_required = false,
    rebuild_reason = NULL,
    updated_at = now()
WHERE tenant_id = $1`, tenantID, recordedAt, eventID); err != nil {
		t.Fatal(err)
	}
}

func assertNoCounter(t *testing.T, pool *pgxpool.Pool, grain, predicate string) {
	t.Helper()
	assertCounter(t, pool, grain, predicate, 0)
}

func assertAllCountersStamped(t *testing.T, pool *pgxpool.Pool, runID string, watermark time.Time) {
	t.Helper()
	var bad int64
	if err := pool.QueryRow(context.Background(), `
SELECT count(*)
FROM goat_identity_counters
WHERE tenant_id = $1
  AND (
    source_import_run_id IS DISTINCT FROM $2::uuid
    OR as_of_recorded_at IS DISTINCT FROM $3::timestamptz
    OR is_rebuilding
  )`, meshaTenant, runID, watermark).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("unexpected unstamped/rebuilding counters=%d", bad)
	}
}

func applyMigrations(t *testing.T, container string) {
	t.Helper()
	root := repoRoot(t)
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			t.Fatal(err)
		}
		psql(t, container, extractGooseUp(string(sqlBytes)))
	}
}

func openPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runOutput(t, "docker", "port", container, "5432/tcp")
	parts := strings.Split(strings.TrimSpace(out), ":")
	port := parts[len(parts)-1]
	url := "postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable"
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func psql(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql failed: %v\n%s\nSQL:\n%s", err, out, sqlText)
	}
}

func extractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(line, "-- +goose Down"):
			inUp = false
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repo root not found")
		}
		dir = next
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func strPtr(value string) *string {
	return &value
}
