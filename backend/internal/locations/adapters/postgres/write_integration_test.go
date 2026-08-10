package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testPostgresImage = "postgres:16.9-alpine"
	testTenantID      = "00000000-0000-4000-8000-000000000001"
	testActorID       = "90000000-0000-4000-8000-000000000001"
	testCBELocation   = "00000000-0000-4000-8000-000000003001"
)

func TestLocationWritePathWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool, repo := startLocationWriteDB(t, ctx)
	defer pool.Close()

	t.Run("create writes audit invalidations and idempotent replay", func(t *testing.T) {
		cmd := createLocationCommand("idem-location-create-0001", "Synthetic Integration Shed")
		result, err := repo.CreateLocation(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateLocation: %v", err)
		}
		if result.Replayed || result.Location.LocationType != "shed" || result.Location.ParentLocationID == nil || *result.Location.ParentLocationID != testCBELocation {
			t.Fatalf("unexpected create result: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE resource_type = 'location' AND resource_id = $1::uuid`, result.Location.LocationID); got != 1 {
			t.Fatalf("audit rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_projection_invalidations WHERE affected_location_id = $1::uuid`, result.Location.LocationID); got != 5 {
			t.Fatalf("projection invalidations = %d", got)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, result.Location.LocationID)

		replay, err := repo.CreateLocation(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateLocation replay: %v", err)
		}
		if !replay.Replayed || replay.Location.LocationID != result.Location.LocationID || replay.FirstResultID == nil || *replay.FirstResultID != result.Location.LocationID {
			t.Fatalf("unexpected replay result: %#v", replay)
		}

		conflicting := cmd
		conflicting.RequestHash = "sha256:different-request-body"
		if _, err := repo.CreateLocation(ctx, conflicting); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("update enforces optimistic concurrency and seeded guard", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-update-seed", "Synthetic Update Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		newName := "Synthetic Update Shed Renamed"
		update := ports.UpdateLocationCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-update-0001",
			StoredIdempotencyKey: testTenantID + ":updateLocation:" + created.Location.LocationID + ":idem-location-update-0001",
			IdempotencyScope:     "updateLocation",
			RequestHash:          "sha256:update-location-0001",
			TraceID:              "trace-location-update-0001",
			LocationID:           created.Location.LocationID,
			Name:                 &newName,
			RowVersion:           created.Location.RowVersion,
		}
		updated, err := repo.UpdateLocation(ctx, update)
		if err != nil {
			t.Fatalf("UpdateLocation: %v", err)
		}
		if updated.Location.Name != newName || updated.Location.RowVersion != created.Location.RowVersion+1 {
			t.Fatalf("unexpected update result: %#v", updated)
		}

		stale := update
		stale.ClientIdempotencyKey = "idem-location-update-stale"
		stale.StoredIdempotencyKey = testTenantID + ":updateLocation:" + created.Location.LocationID + ":idem-location-update-stale"
		stale.RequestHash = "sha256:update-location-stale"
		if _, err := repo.UpdateLocation(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale write conflict, got %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, stale.StoredIdempotencyKey); got != 0 {
			t.Fatalf("stale write left idempotency rows = %d", got)
		}

		seededName := "Bad Seeded Rename"
		seeded := update
		seeded.LocationID = testCBELocation
		seeded.Name = &seededName
		seeded.RowVersion = locationRowVersion(t, pool, testCBELocation)
		seeded.ClientIdempotencyKey = "idem-location-seeded-guard"
		seeded.StoredIdempotencyKey = testTenantID + ":updateLocation:" + testCBELocation + ":idem-location-seeded-guard"
		seeded.RequestHash = "sha256:seeded-guard"
		if _, err := repo.UpdateLocation(ctx, seeded); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected seeded guard conflict, got %v", err)
		}
	})

	t.Run("shed operational update cascades to mapped active partition pens", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-pen-op-cascade-shed", "Synthetic Pen Cascade Shed"))
		if err != nil {
			t.Fatalf("CreateLocation shed: %v", err)
		}
		penCmd := createLocationCommand("idem-location-pen-op-cascade-pen", "Synthetic Pen Cascade Shed - Part 1")
		penCmd.LocationType = "pen"
		penCmd.ParentLocationID = stringPtr(created.Location.LocationID)
		pen, err := repo.CreateLocation(ctx, penCmd)
		if err != nil {
			t.Fatalf("CreateLocation pen: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES (
  $1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual', $3::uuid
)`, testTenantID, created.Location.LocationID, pen.Location.LocationID); err != nil {
			t.Fatalf("seed shed partition: %v", err)
		}
		op := domain.OperationalAttributes{
			UsableForCounts:      false,
			UsableForFeed:        false,
			UsableForVaccination: false,
			UsableForSOP:         false,
			IsQuarantine:         true,
			IsICU:                true,
			DisplayOrder:         17,
			Notes:                stringPtr("quarantine cascade"),
		}
		update := ports.UpdateLocationCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-pen-op-cascade-update",
			StoredIdempotencyKey: testTenantID + ":updateLocation:" + created.Location.LocationID + ":idem-location-pen-op-cascade-update",
			IdempotencyScope:     "updateLocation",
			RequestHash:          "sha256:pen-op-cascade-update",
			TraceID:              "trace-pen-op-cascade-update",
			LocationID:           created.Location.LocationID,
			Operational:          &op,
			RowVersion:           created.Location.RowVersion,
		}
		if _, err := repo.UpdateLocation(ctx, update); err != nil {
			t.Fatalf("UpdateLocation operational: %v", err)
		}
		var usableForVaccination, isQuarantine, isICU bool
		if err := pool.QueryRow(ctx, `
SELECT usable_for_vaccination, is_quarantine, is_icu
FROM location_operational_attributes
WHERE tenant_id=$1::uuid AND location_id=$2::uuid`,
			testTenantID, pen.Location.LocationID).Scan(&usableForVaccination, &isQuarantine, &isICU); err != nil {
			t.Fatalf("read pen operational attributes: %v", err)
		}
		if usableForVaccination || !isQuarantine || !isICU {
			t.Fatalf("pen operational attributes did not cascade: usable=%v quarantine=%v icu=%v", usableForVaccination, isQuarantine, isICU)
		}
	})

	t.Run("retire blocks empty mapped partition pen", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-retire-mapped-shed", "Synthetic Retire Mapped Shed"))
		if err != nil {
			t.Fatalf("CreateLocation shed: %v", err)
		}
		penCmd := createLocationCommand("idem-location-retire-mapped-pen", "Synthetic Retire Mapped Shed - Part 1")
		penCmd.LocationType = "pen"
		penCmd.ParentLocationID = stringPtr(created.Location.LocationID)
		pen, err := repo.CreateLocation(ctx, penCmd)
		if err != nil {
			t.Fatalf("CreateLocation pen: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (
  tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id
) VALUES (
  $1::uuid, $2::uuid, 'Part 1', '1', 'active', 'manual', $3::uuid
)`, testTenantID, created.Location.LocationID, pen.Location.LocationID); err != nil {
			t.Fatalf("seed shed partition: %v", err)
		}
		if _, err := repo.RetireLocation(ctx, ports.RetireLocationCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-retire-mapped-pen",
			StoredIdempotencyKey: testTenantID + ":retireLocation:" + pen.Location.LocationID + ":idem-location-retire-mapped-pen",
			IdempotencyScope:     "retireLocation",
			RequestHash:          "sha256:retire-mapped-pen",
			TraceID:              "trace-retire-mapped-pen",
			LocationID:           pen.Location.LocationID,
			Reason:               "Synthetic empty mapped pen should still block.",
			RowVersion:           pen.Location.RowVersion,
		}); !errors.Is(err, ports.ErrBlockingUsage) {
			t.Fatalf("expected mapped pen retirement to block, got %v", err)
		}
	})

	t.Run("hard delete removes unreferenced staging location", func(t *testing.T) {
		cmd := createLocationCommand("idem-location-delete-seed", "Synthetic Delete Shed")
		cmd.Status = "staging"
		created, err := repo.CreateLocation(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		deleteCmd := ports.DeleteLocationCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-delete-0001",
			StoredIdempotencyKey: testTenantID + ":deleteLocation:" + created.Location.LocationID + ":idem-location-delete-0001",
			IdempotencyScope:     "deleteLocation",
			RequestHash:          "sha256:delete-location-0001",
			TraceID:              "trace-location-delete-0001",
			LocationID:           created.Location.LocationID,
			Reason:               "Synthetic hard-delete test.",
			RowVersion:           created.Location.RowVersion,
		}
		deleted, err := repo.DeleteLocation(ctx, deleteCmd)
		if err != nil {
			t.Fatalf("DeleteLocation: %v", err)
		}
		if deleted.ResourceType != "location" || deleted.ResourceID != created.Location.LocationID {
			t.Fatalf("unexpected delete result: %#v", deleted)
		}
		if _, err := repo.GetLocation(ctx, testTenantID, created.Location.LocationID); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected deleted location not found, got %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE resource_type = 'location' AND resource_id = $1::uuid AND action = 'locations.location.deleted'`, created.Location.LocationID); got != 1 {
			t.Fatalf("delete audit rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_projection_invalidations WHERE affected_location_id IS NULL AND reason = 'location_deleted' AND source_ref LIKE '%' || $1`, created.Location.LocationID); got != 5 {
			t.Fatalf("delete invalidation rows = %d", got)
		}

		replay, err := repo.DeleteLocation(ctx, deleteCmd)
		if err != nil {
			t.Fatalf("DeleteLocation replay: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != created.Location.LocationID {
			t.Fatalf("unexpected delete replay: %#v", replay)
		}
	})

	t.Run("alias uniqueness capacity overlap and review resolution", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-governance-seed", "Synthetic Governance Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		aliasCmd := ports.CreateLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-0001",
			StoredIdempotencyKey: testTenantID + ":createLocationAlias:" + created.Location.LocationID + ":idem-location-alias-0001",
			IdempotencyScope:     "createLocationAlias",
			RequestHash:          "sha256:create-alias-0001",
			TraceID:              "trace-location-alias-0001",
			LocationID:           created.Location.LocationID,
			AliasCode:            "Synthetic Alias",
			SourceContext:        "manual",
		}
		alias, err := repo.CreateLocationAlias(ctx, aliasCmd)
		if err != nil {
			t.Fatalf("CreateLocationAlias: %v", err)
		}
		if alias.Alias.AliasCode != aliasCmd.AliasCode || alias.Alias.CanonicalLocationID != created.Location.LocationID {
			t.Fatalf("unexpected alias result: %#v", alias)
		}
		duplicateAlias := aliasCmd
		duplicateAlias.ClientIdempotencyKey = "idem-location-alias-duplicate"
		duplicateAlias.StoredIdempotencyKey = testTenantID + ":createLocationAlias:" + created.Location.LocationID + ":idem-location-alias-duplicate"
		duplicateAlias.RequestHash = "sha256:create-alias-duplicate"
		duplicateAlias.AliasCode = "synthetic alias"
		if _, err := repo.CreateLocationAlias(ctx, duplicateAlias); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected alias uniqueness conflict, got %v", err)
		}

		capacityCmd := ports.CreateLocationCapacityCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-capacity-0001",
			StoredIdempotencyKey: testTenantID + ":createLocationCapacity:" + created.Location.LocationID + ":idem-location-capacity-0001",
			IdempotencyScope:     "createLocationCapacity",
			RequestHash:          "sha256:create-capacity-0001",
			TraceID:              "trace-location-capacity-0001",
			LocationID:           created.Location.LocationID,
			CapacityKind:         "goat_occupancy",
			CapacityValue:        42,
			EffectiveFrom:        "2026-01-01",
			Source:               "manual",
		}
		if _, err := repo.CreateLocationCapacity(ctx, capacityCmd); err != nil {
			t.Fatalf("CreateLocationCapacity: %v", err)
		}
		overlap := capacityCmd
		overlap.ClientIdempotencyKey = "idem-location-capacity-overlap"
		overlap.StoredIdempotencyKey = testTenantID + ":createLocationCapacity:" + created.Location.LocationID + ":idem-location-capacity-overlap"
		overlap.RequestHash = "sha256:create-capacity-overlap"
		overlap.EffectiveFrom = "2026-06-01"
		if _, err := repo.CreateLocationCapacity(ctx, overlap); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected capacity overlap conflict, got %v", err)
		}

		sourceContext := "sheds_db"
		sourceLabel := "UNKNOWN-GOVERNANCE-SHED"
		review, err := repo.CreateLocationReviewItem(ctx, ports.CreateLocationReviewItemCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-review-0001",
			StoredIdempotencyKey: testTenantID + ":createLocationReviewItem:review-evidence-0001:idem-location-review-0001",
			IdempotencyScope:     "createLocationReviewItem",
			RequestHash:          "sha256:create-review-0001",
			TraceID:              "trace-location-review-0001",
			ReviewType:           "unknown_alias",
			SourceContext:        &sourceContext,
			SourceLabel:          &sourceLabel,
			EvidenceJSON:         []byte(`{"source":"sheds_db","label":"UNKNOWN-GOVERNANCE-SHED"}`),
			EvidenceHash:         "review-evidence-0001",
		})
		if err != nil {
			t.Fatalf("CreateLocationReviewItem: %v", err)
		}
		resolved, err := repo.ResolveLocationReviewItem(ctx, ports.ResolveLocationReviewItemCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-review-resolve-0001",
			StoredIdempotencyKey: testTenantID + ":resolveLocationReviewItem:" + review.ReviewItem.ReviewID + ":idem-location-review-resolve-0001",
			IdempotencyScope:     "resolveLocationReviewItem",
			RequestHash:          "sha256:resolve-review-0001",
			TraceID:              "trace-location-review-resolve-0001",
			ReviewID:             review.ReviewItem.ReviewID,
			Status:               "resolved",
			CanonicalLocationID:  &created.Location.LocationID,
			ResolutionNotes:      "Synthetic test resolution.",
			RowVersion:           review.ReviewItem.RowVersion,
		})
		if err != nil {
			t.Fatalf("ResolveLocationReviewItem: %v", err)
		}
		if resolved.ReviewItem.Status != "resolved" || resolved.ReviewItem.ResolvedAt == nil {
			t.Fatalf("unexpected resolved review item: %#v", resolved)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_aliases WHERE canonical_location_id = $1::uuid AND alias_code = $2 AND source_context = $3`, created.Location.LocationID, sourceLabel, sourceContext); got != 1 {
			t.Fatalf("resolved review alias rows = %d", got)
		}
	})

	t.Run("alias update retire and delete are reachable", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-alias-crud-seed", "Synthetic Alias CRUD Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		alias, err := repo.CreateLocationAlias(ctx, ports.CreateLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-crud-create",
			StoredIdempotencyKey: testTenantID + ":createLocationAlias:" + created.Location.LocationID + ":idem-location-alias-crud-create",
			IdempotencyScope:     "createLocationAlias",
			RequestHash:          "sha256:create-alias-crud",
			TraceID:              "trace-location-alias-crud-create",
			LocationID:           created.Location.LocationID,
			AliasCode:            "Alias CRUD A",
			SourceContext:        "manual",
		})
		if err != nil {
			t.Fatalf("CreateLocationAlias: %v", err)
		}
		notes := "Updated by integration test."
		updated, err := repo.UpdateLocationAlias(ctx, ports.UpdateLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-crud-update",
			StoredIdempotencyKey: testTenantID + ":updateLocationAlias:" + alias.Alias.AliasID + ":idem-location-alias-crud-update",
			IdempotencyScope:     "updateLocationAlias",
			RequestHash:          "sha256:update-alias-crud",
			TraceID:              "trace-location-alias-crud-update",
			LocationID:           created.Location.LocationID,
			AliasID:              alias.Alias.AliasID,
			AliasCode:            "Alias CRUD B",
			SourceContext:        "manual",
			Notes:                &notes,
			RowVersion:           alias.Alias.RowVersion,
		})
		if err != nil {
			t.Fatalf("UpdateLocationAlias: %v", err)
		}
		if updated.Alias.AliasCode != "Alias CRUD B" || updated.Alias.RowVersion != alias.Alias.RowVersion+1 {
			t.Fatalf("unexpected updated alias: %#v", updated.Alias)
		}
		if _, err := repo.DeleteLocationAlias(ctx, ports.DeleteLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-crud-delete-active",
			StoredIdempotencyKey: testTenantID + ":deleteLocationAlias:" + updated.Alias.AliasID + ":idem-location-alias-crud-delete-active",
			IdempotencyScope:     "deleteLocationAlias",
			RequestHash:          "sha256:delete-active-alias-crud",
			TraceID:              "trace-location-alias-crud-delete-active",
			LocationID:           created.Location.LocationID,
			AliasID:              updated.Alias.AliasID,
			Reason:               "Synthetic active alias delete should block.",
			RowVersion:           updated.Alias.RowVersion,
		}); !errors.Is(err, ports.ErrBlockingUsage) {
			t.Fatalf("expected active alias delete to block, got %v", err)
		}
		retired, err := repo.RetireLocationAlias(ctx, ports.RetireLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-crud-retire",
			StoredIdempotencyKey: testTenantID + ":retireLocationAlias:" + updated.Alias.AliasID + ":idem-location-alias-crud-retire",
			IdempotencyScope:     "retireLocationAlias",
			RequestHash:          "sha256:retire-alias-crud",
			TraceID:              "trace-location-alias-crud-retire",
			LocationID:           created.Location.LocationID,
			AliasID:              updated.Alias.AliasID,
			Reason:               "Synthetic retire before delete.",
			RowVersion:           updated.Alias.RowVersion,
		})
		if err != nil {
			t.Fatalf("RetireLocationAlias: %v", err)
		}
		if retired.Alias.Status != "retired" {
			t.Fatalf("alias not retired: %#v", retired.Alias)
		}
		deleted, err := repo.DeleteLocationAlias(ctx, ports.DeleteLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-alias-crud-delete",
			StoredIdempotencyKey: testTenantID + ":deleteLocationAlias:" + retired.Alias.AliasID + ":idem-location-alias-crud-delete",
			IdempotencyScope:     "deleteLocationAlias",
			RequestHash:          "sha256:delete-alias-crud",
			TraceID:              "trace-location-alias-crud-delete",
			LocationID:           created.Location.LocationID,
			AliasID:              retired.Alias.AliasID,
			Reason:               "Synthetic alias hard delete.",
			RowVersion:           retired.Alias.RowVersion,
		})
		if err != nil {
			t.Fatalf("DeleteLocationAlias: %v", err)
		}
		if deleted.ResourceType != "location_alias" || deleted.ResourceID != retired.Alias.AliasID {
			t.Fatalf("unexpected alias delete result: %#v", deleted)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_aliases WHERE alias_id = $1::uuid`, retired.Alias.AliasID); got != 0 {
			t.Fatalf("alias rows after delete = %d", got)
		}
	})

	t.Run("capacity update and delete are reachable", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-capacity-crud-seed", "Synthetic Capacity CRUD Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		capacity, err := repo.CreateLocationCapacity(ctx, ports.CreateLocationCapacityCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-capacity-crud-create",
			StoredIdempotencyKey: testTenantID + ":createLocationCapacity:" + created.Location.LocationID + ":idem-location-capacity-crud-create",
			IdempotencyScope:     "createLocationCapacity",
			RequestHash:          "sha256:create-capacity-crud",
			TraceID:              "trace-location-capacity-crud-create",
			LocationID:           created.Location.LocationID,
			CapacityKind:         "goat_occupancy",
			CapacityValue:        12,
			EffectiveFrom:        "2027-01-01",
			Source:               "manual",
		})
		if err != nil {
			t.Fatalf("CreateLocationCapacity: %v", err)
		}
		effectiveTo := "2027-12-31"
		sourceRef := "capacity-crud-ref"
		notes := "Capacity updated by integration test."
		updated, err := repo.UpdateLocationCapacity(ctx, ports.UpdateLocationCapacityCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-capacity-crud-update",
			StoredIdempotencyKey: testTenantID + ":updateLocationCapacity:" + capacity.Capacity.CapacityRecordID + ":idem-location-capacity-crud-update",
			IdempotencyScope:     "updateLocationCapacity",
			RequestHash:          "sha256:update-capacity-crud",
			TraceID:              "trace-location-capacity-crud-update",
			LocationID:           created.Location.LocationID,
			CapacityRecordID:     capacity.Capacity.CapacityRecordID,
			CapacityKind:         "goat_occupancy",
			CapacityValue:        18,
			EffectiveFrom:        "2027-01-01",
			EffectiveTo:          &effectiveTo,
			Source:               "manual",
			SourceRef:            &sourceRef,
			Notes:                &notes,
			RowVersion:           capacity.Capacity.RowVersion,
		})
		if err != nil {
			t.Fatalf("UpdateLocationCapacity: %v", err)
		}
		if updated.Capacity.CapacityValue != 18 || updated.Capacity.EffectiveTo == nil || *updated.Capacity.EffectiveTo != effectiveTo {
			t.Fatalf("unexpected updated capacity: %#v", updated.Capacity)
		}
		deleted, err := repo.DeleteLocationCapacity(ctx, ports.DeleteLocationCapacityCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-capacity-crud-delete",
			StoredIdempotencyKey: testTenantID + ":deleteLocationCapacity:" + updated.Capacity.CapacityRecordID + ":idem-location-capacity-crud-delete",
			IdempotencyScope:     "deleteLocationCapacity",
			RequestHash:          "sha256:delete-capacity-crud",
			TraceID:              "trace-location-capacity-crud-delete",
			LocationID:           created.Location.LocationID,
			CapacityRecordID:     updated.Capacity.CapacityRecordID,
			Reason:               "Synthetic capacity hard delete.",
			RowVersion:           updated.Capacity.RowVersion,
		})
		if err != nil {
			t.Fatalf("DeleteLocationCapacity: %v", err)
		}
		if deleted.ResourceType != "location_capacity_record" || deleted.ResourceID != updated.Capacity.CapacityRecordID {
			t.Fatalf("unexpected capacity delete result: %#v", deleted)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_capacity_records WHERE capacity_record_id = $1::uuid`, updated.Capacity.CapacityRecordID); got != 0 {
			t.Fatalf("capacity rows after delete = %d", got)
		}
	})

	t.Run("sheds db source evidence persists for aliases and capacity", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-sheds-db-source-seed", "Synthetic Sheds DB Source Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		notes := "Reviewed Sheds DB source label."
		if _, err := repo.CreateLocationAlias(ctx, ports.CreateLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-sheds-db-alias-create",
			StoredIdempotencyKey: testTenantID + ":createLocationAlias:" + created.Location.LocationID + ":idem-location-sheds-db-alias-create",
			IdempotencyScope:     "createLocationAlias",
			RequestHash:          "sha256:create-sheds-db-alias",
			TraceID:              "trace-location-sheds-db-alias-create",
			LocationID:           created.Location.LocationID,
			AliasCode:            "Gandhi 1 - Part 1",
			SourceContext:        "sheds_db",
			Notes:                &notes,
		}); err != nil {
			t.Fatalf("CreateLocationAlias: %v", err)
		}
		sourceRef := "Sheds DB.xlsx#DB!A3:H1015"
		if _, err := repo.CreateLocationCapacity(ctx, ports.CreateLocationCapacityCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-sheds-db-capacity-create",
			StoredIdempotencyKey: testTenantID + ":createLocationCapacity:" + created.Location.LocationID + ":idem-location-sheds-db-capacity-create",
			IdempotencyScope:     "createLocationCapacity",
			RequestHash:          "sha256:create-sheds-db-capacity",
			TraceID:              "trace-location-sheds-db-capacity-create",
			LocationID:           created.Location.LocationID,
			CapacityKind:         "goat_occupancy",
			CapacityValue:        25,
			EffectiveFrom:        "2026-06-30",
			Source:               "sheds_db",
			SourceRef:            &sourceRef,
		}); err != nil {
			t.Fatalf("CreateLocationCapacity: %v", err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_aliases WHERE canonical_location_id = $1::uuid AND source_context = 'sheds_db'`, created.Location.LocationID); got != 1 {
			t.Fatalf("sheds_db alias rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_capacity_records WHERE location_id = $1::uuid AND source = 'sheds_db' AND capacity_value = 25`, created.Location.LocationID); got != 1 {
			t.Fatalf("sheds_db capacity rows = %d", got)
		}
	})

	t.Run("source label resolver resolves aliases and opens reusable unknown reviews", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-resolver-seed", "Synthetic Resolver Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		if _, err := repo.CreateLocationAlias(ctx, ports.CreateLocationAliasCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-resolver-alias-0001",
			StoredIdempotencyKey: testTenantID + ":createLocationAlias:" + created.Location.LocationID + ":idem-location-resolver-alias-0001",
			IdempotencyScope:     "createLocationAlias",
			RequestHash:          "sha256:create-resolver-alias-0001",
			TraceID:              "trace-location-resolver-alias-0001",
			LocationID:           created.Location.LocationID,
			AliasCode:            "Sheds DB Shed A",
			SourceContext:        "sheds_db",
		}); err != nil {
			t.Fatalf("CreateLocationAlias resolver setup: %v", err)
		}

		service := locationsapp.NewService(repo)
		resolved, err := service.ResolveSourceLabel(ctx, locationsapp.ResolveSourceLabelInput{
			TenantID:      testTenantID,
			ActorID:       testActorID,
			TraceID:       "trace-resolve-sheds-label",
			SourceContext: "sheds_db",
			SourceLabel:   "  SHEDS DB SHED A  ",
		})
		if err != nil {
			t.Fatalf("ResolveSourceLabel resolved: %v", err)
		}
		if resolved.Status != "resolved" || resolved.Location == nil || resolved.Location.LocationID != created.Location.LocationID || resolved.NormalizedSourceLabel != "sheds db shed a" {
			t.Fatalf("unexpected resolved source label: %#v", resolved)
		}

		unknown, err := service.ResolveSourceLabel(ctx, locationsapp.ResolveSourceLabelInput{
			TenantID:      testTenantID,
			TraceID:       "trace-resolve-mortality-label",
			SourceContext: "import",
			SourceLabel:   "Unmapped Housing 9",
			EvidenceJSON:  []byte(`{"source":"import","column":"housing","label":"Unmapped Housing 9"}`),
			EvidenceHash:  "import-unmapped-housing-9",
		})
		if err != nil {
			t.Fatalf("ResolveSourceLabel unknown: %v", err)
		}
		if unknown.Status != "review_required" || unknown.ReviewItem == nil || unknown.ReviewItem.ReviewType != "unknown_alias" {
			t.Fatalf("unexpected unknown source label: %#v", unknown)
		}
		if unknown.ReviewItem.NormalizedSourceLabel == nil || *unknown.ReviewItem.NormalizedSourceLabel != "unmapped housing 9" {
			t.Fatalf("normalized label missing from review item: %#v", unknown.ReviewItem.NormalizedSourceLabel)
		}

		reused, err := service.ResolveSourceLabel(ctx, locationsapp.ResolveSourceLabelInput{
			TenantID:      testTenantID,
			TraceID:       "trace-resolve-mortality-label-reuse",
			SourceContext: "import",
			SourceLabel:   "Unmapped Housing 9",
			EvidenceJSON:  []byte(`{"source":"import","column":"housing","label":"Unmapped Housing 9"}`),
			EvidenceHash:  "import-unmapped-housing-9",
		})
		if err != nil {
			t.Fatalf("ResolveSourceLabel unknown reuse: %v", err)
		}
		if reused.ReviewItem == nil || reused.ReviewItem.ReviewID != unknown.ReviewItem.ReviewID {
			t.Fatalf("expected review item reuse, got first=%#v reused=%#v", unknown.ReviewItem, reused.ReviewItem)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM location_review_items WHERE tenant_id = $1::uuid AND source_context = 'import' AND normalized_source_label = 'unmapped housing 9' AND status = 'open'`, testTenantID); got != 1 {
			t.Fatalf("open review rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE resource_type = 'location_review_item' AND resource_id = $1::uuid AND actor_type = 'system'`, unknown.ReviewItem.ReviewID); got != 1 {
			t.Fatalf("system audit rows for resolver review = %d", got)
		}
	})

	t.Run("review-resolved aliases use normalized matching", func(t *testing.T) {
		created, err := repo.CreateLocation(ctx, createLocationCommand("idem-location-normalized-review-seed", "Synthetic Normalized Review Shed"))
		if err != nil {
			t.Fatalf("CreateLocation setup: %v", err)
		}
		sourceContext := "sheds_db"
		sourceLabel := "  Sheds   DB   Shed   Normalized  "
		normalized := locationsapp.NormalizeSourceLabel(sourceLabel)
		review, err := repo.CreateLocationReviewItem(ctx, ports.CreateLocationReviewItemCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-normalized-review-create",
			StoredIdempotencyKey:  testTenantID + ":createLocationReviewItem:normalized-review-evidence:idem-location-normalized-review-create",
			IdempotencyScope:      "createLocationReviewItem",
			RequestHash:           "sha256:create-normalized-review",
			TraceID:               "trace-location-normalized-review-create",
			ReviewType:            "unknown_alias",
			SourceContext:         &sourceContext,
			SourceLabel:           &sourceLabel,
			NormalizedSourceLabel: &normalized,
			EvidenceJSON:          []byte(`{"source":"sheds_db","label":"Sheds DB Shed Normalized"}`),
			EvidenceHash:          "normalized-review-evidence",
		})
		if err != nil {
			t.Fatalf("CreateLocationReviewItem: %v", err)
		}
		if _, err := repo.ResolveLocationReviewItem(ctx, ports.ResolveLocationReviewItemCommand{
			TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: "idem-location-normalized-review-resolve",
			StoredIdempotencyKey: testTenantID + ":resolveLocationReviewItem:" + review.ReviewItem.ReviewID + ":idem-location-normalized-review-resolve",
			IdempotencyScope:     "resolveLocationReviewItem",
			RequestHash:          "sha256:resolve-normalized-review",
			TraceID:              "trace-location-normalized-review-resolve",
			ReviewID:             review.ReviewItem.ReviewID,
			Status:               "resolved",
			CanonicalLocationID:  &created.Location.LocationID,
			ResolutionNotes:      "Synthetic normalized alias resolution.",
			RowVersion:           review.ReviewItem.RowVersion,
		}); err != nil {
			t.Fatalf("ResolveLocationReviewItem: %v", err)
		}
		matches, err := repo.FindActiveAliases(ctx, ports.SourceLabelQuery{
			TenantID:              testTenantID,
			SourceContext:         sourceContext,
			NormalizedSourceLabel: normalized,
		})
		if err != nil {
			t.Fatalf("FindActiveAliases: %v", err)
		}
		if len(matches) != 1 || matches[0].Location.LocationID != created.Location.LocationID {
			t.Fatalf("unexpected normalized alias matches: %#v", matches)
		}
	})
}

func startLocationWriteDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	container := "goatos-location-write-test-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = testPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	ready := false
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
	}
	applyMigrations(t, container)
	pool := openPool(t, ctx, container)
	return pool, NewRepository(pool, 5*time.Second)
}

func createLocationCommand(key, name string) ports.CreateLocationCommand {
	locationCode := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	return ports.CreateLocationCommand{
		TenantID: testTenantID, ActorID: testActorID, ClientIdempotencyKey: key,
		StoredIdempotencyKey: testTenantID + ":createLocation:" + key,
		IdempotencyScope:     "createLocation",
		RequestHash:          "sha256:" + key,
		TraceID:              "trace-" + key,
		LocationType:         "shed",
		LocationCode:         &locationCode,
		Name:                 name,
		ParentLocationID:     stringPtr(testCBELocation),
		Status:               "active",
		Country:              "IN",
		Timezone:             "Asia/Kolkata",
		Operational: domain.OperationalAttributes{
			UsableForCounts:      true,
			UsableForFeed:        true,
			UsableForVaccination: true,
			UsableForSOP:         true,
		},
	}
}

func locationRowVersion(t *testing.T, pool *pgxpool.Pool, locationID string) int {
	t.Helper()
	var rowVersion int
	if err := pool.QueryRow(context.Background(), `SELECT row_version FROM locations WHERE location_id = $1::uuid`, locationID).Scan(&rowVersion); err != nil {
		t.Fatal(err)
	}
	return rowVersion
}

func assertIdempotencyCompleted(t *testing.T, pool *pgxpool.Pool, key, resultID string) {
	t.Helper()
	var status, storedResultID string
	if err := pool.QueryRow(context.Background(), `SELECT status, result_id::text FROM idempotency_keys WHERE idempotency_key = $1`, key).Scan(&status, &storedResultID); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || storedResultID != resultID {
		t.Fatalf("idempotency row = status %q result %q, want completed %q", status, storedResultID, resultID)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v\n%s", err, stderr.String())
	}
}

func extractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(line, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(line, "-- +goose Down") {
			break
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "backend", "migrations", "postgres")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("repo root not found")
		}
		wd = next
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
	return string(out)
}
