package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	locationpg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationapp "github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID    = "00000000-0000-4000-8000-000000000001"
	testActorID     = "90000000-0000-4000-8000-000000000001"
	testCBELocation = "00000000-0000-4000-8000-000000003001"
)

func TestParseRequiredProfilesDefaultsAndSorts(t *testing.T) {
	got, err := parseRequiredProfiles(strings.NewReader(`{
		"source_ref": "context/source-findings/sheds-db-source-findings.md",
		"required_profiles": [
			{"location_id": "00000000-0000-4000-8000-000000000102", "alias_code": "Ho Chi Minh 1", "capacity_value": 50, "effective_from": "2026-06-30"},
			{"location_id": "00000000-0000-4000-8000-000000000101", "alias_code": "Gandhi 1 - Part 1", "capacity_value": 25, "effective_from": "2026-06-30"}
		]
	}`))
	if err != nil {
		t.Fatalf("parseRequiredProfiles: %v", err)
	}
	if got.SourceRef != "context/source-findings/sheds-db-source-findings.md" {
		t.Fatalf("source_ref=%q", got.SourceRef)
	}
	first := got.RequiredProfiles[0]
	if first.LocationID != "00000000-0000-4000-8000-000000000101" || first.SourceContext != "sheds_db" || first.Source != "sheds_db" || first.CapacityKind != "goat_occupancy" {
		t.Fatalf("first profile=%+v, want sorted/defaulted Gandhi profile", first)
	}
}

func TestParseRequiredProfilesRejectsDuplicateNormalizedProfile(t *testing.T) {
	_, err := parseRequiredProfiles(strings.NewReader(`{
		"required_profiles": [
			{"location_id": "loc-1", "alias_code": "Gandhi   1", "capacity_value": 25, "effective_from": "2026-06-30"},
			{"location_id": "loc-1", "alias_code": "Gandhi 1", "capacity_value": 25, "effective_from": "2026-06-30"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate required profile") {
		t.Fatalf("err=%v, want duplicate required profile", err)
	}
}

func TestCoverageStatusNeverMarksCSG7Ready(t *testing.T) {
	status, blocker := coverageStatus("source.md", coverageResult{Total: 2})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "full source workbook parity") || !strings.Contains(blocker, "seeded local E2E remain") {
		t.Fatalf("blocker=%q, want remaining-evidence caveat", blocker)
	}
}

func TestCoverageStatusBlocksMissingProfiles(t *testing.T) {
	status, blocker := coverageStatus("source.md", coverageResult{
		Total: 2,
		Missing: []missingProfile{{
			Profile: requiredProfile{LocationID: "loc-1", SourceContext: "sheds_db", AliasCode: "Gandhi 1"},
			Reason:  "missing_capacity",
		}},
	})
	if status != "blocked" {
		t.Fatalf("status=%s, want blocked", status)
	}
	if !strings.Contains(blocker, "loc-1/sheds_db/Gandhi 1/missing_capacity") {
		t.Fatalf("blocker=%q, want missing capacity details", blocker)
	}
}

func TestCheckProfileCoverageRequiresAliasAndCapacity(t *testing.T) {
	db := fakeProfileDB{
		aliases: map[string]bool{
			"loc-1\x00sheds_db\x00gandhi 1": true,
			"loc-2\x00sheds_db\x00castro 1": true,
		},
		capacities: map[string]bool{
			"loc-1\x00goat_occupancy\x0025\x002026-06-30\x00sheds_db": true,
		},
	}
	required := []requiredProfile{
		{LocationID: "loc-1", AliasCode: "Gandhi 1", SourceContext: "sheds_db", CapacityKind: "goat_occupancy", CapacityValue: 25, EffectiveFrom: "2026-06-30", Source: "sheds_db"},
		{LocationID: "loc-2", AliasCode: "Castro 1", SourceContext: "sheds_db", CapacityKind: "goat_occupancy", CapacityValue: 50, EffectiveFrom: "2026-06-30", Source: "sheds_db"},
		{LocationID: "loc-3", AliasCode: "Mandela 1", SourceContext: "sheds_db", CapacityKind: "goat_occupancy", CapacityValue: 30, EffectiveFrom: "2026-06-30", Source: "sheds_db"},
	}

	result, err := checkProfileCoverage(context.Background(), &db, "tenant-1", required)
	if err != nil {
		t.Fatalf("checkProfileCoverage: %v", err)
	}
	if result.Total != 3 || len(result.Missing) != 2 {
		t.Fatalf("result=%+v, want two missing profiles", result)
	}
	if result.Missing[0].Reason != "missing_capacity" || result.Missing[1].Reason != "missing_active_alias" {
		t.Fatalf("missing=%+v, want capacity then alias missing", result.Missing)
	}
}

func TestCheckProfileCoverageSurfacesLookupErrors(t *testing.T) {
	db := fakeProfileDB{err: errors.New("db down")}
	_, err := checkProfileCoverage(context.Background(), &db, "tenant-1", []requiredProfile{{
		LocationID: "loc-1", AliasCode: "Gandhi 1", SourceContext: "sheds_db", CapacityKind: "goat_occupancy", CapacityValue: 25, EffectiveFrom: "2026-06-30", Source: "sheds_db",
	}})
	if err == nil || !strings.Contains(err.Error(), "loc-1/Gandhi 1") {
		t.Fatalf("err=%v, want contextual lookup error", err)
	}
}

func TestUpsertCSG7ReadinessWritesProfileCoverageEvidence(t *testing.T) {
	db := &fakeReadinessDB{}
	err := upsertCSG7Readiness(context.Background(), db, "tenant-1", "source.md", "pending", "still pending")
	if err != nil {
		t.Fatalf("upsertCSG7Readiness: %v", err)
	}
	if !strings.Contains(db.query, "location-profile-coverage-check") {
		t.Fatalf("query=%q, want implementation reference", db.query)
	}
	if len(db.args) != 4 {
		t.Fatalf("args=%+v, want tenant/status/evidence/blocker", db.args)
	}
	if db.args[1] != "pending" {
		t.Fatalf("status arg=%v", db.args[1])
	}
	evidence, _ := db.args[2].(string)
	if !strings.HasPrefix(evidence, "location-profile-coverage-check:source.md:") {
		t.Fatalf("evidence=%q, want command evidence ref", evidence)
	}
}

func TestProfileCoverageAgainstMigratedLocations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	service := locationapp.NewService(locationpg.NewRepository(pool, 5*time.Second))
	created, err := service.CreateLocation(ctx, locationapp.CreateLocationInput{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		IdempotencyKey: "idem-sheds-db-profile-coverage-location",
		TraceID:        "trace-sheds-db-profile-coverage-location",
		RawBody: []byte(fmt.Sprintf(`{
			"location_type": "shed",
			"location_code": "SDB_PROFILE_COVERAGE_1",
			"name": "Sheds DB Profile Coverage Shed",
			"parent_location_id": %q,
			"status": "active",
			"country": "IN",
			"timezone": "Asia/Kolkata",
			"operational": {
				"usable_for_counts": true,
				"usable_for_feed": true,
				"usable_for_vaccination": true,
				"usable_for_sop": true
			}
		}`, testCBELocation)),
	})
	if err != nil {
		t.Fatalf("CreateLocation: %v", err)
	}
	locationID := created.Location.LocationID
	if _, err := service.CreateLocationAlias(ctx, locationapp.CreateLocationAliasInput{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		IdempotencyKey: "idem-sheds-db-profile-coverage-alias",
		TraceID:        "trace-sheds-db-profile-coverage-alias",
		LocationID:     locationID,
		RawBody:        []byte(`{"alias_code":"Gandhi    1","source_context":"sheds_db","notes":"Reviewed Sheds DB alias"}`),
	}); err != nil {
		t.Fatalf("CreateLocationAlias: %v", err)
	}
	if _, err := service.CreateLocationCapacity(ctx, locationapp.CreateLocationCapacityInput{
		TenantID:       testTenantID,
		ActorID:        testActorID,
		IdempotencyKey: "idem-sheds-db-profile-coverage-capacity",
		TraceID:        "trace-sheds-db-profile-coverage-capacity",
		LocationID:     locationID,
		RawBody:        []byte(`{"capacity_kind":"goat_occupancy","capacity_value":25,"effective_from":"2026-06-30","source":"sheds_db","source_ref":"Sheds DB.xlsx#DB!D6"}`),
	}); err != nil {
		t.Fatalf("CreateLocationCapacity: %v", err)
	}

	required := []requiredProfile{{
		LocationID: locationID, AliasCode: "Gandhi 1", SourceContext: "sheds_db",
		CapacityKind: "goat_occupancy", CapacityValue: 25, EffectiveFrom: "2026-06-30", Source: "sheds_db",
	}}
	result, err := checkProfileCoverage(ctx, pool, testTenantID, required)
	if err != nil {
		t.Fatalf("checkProfileCoverage: %v", err)
	}
	if result.Total != 1 || len(result.Missing) != 0 {
		t.Fatalf("result=%+v, want covered profile", result)
	}
	status, blocker := coverageStatus("backend/testdata/locations/sheds-db-profile-coverage-sample.json", result)
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if err := upsertCSG7Readiness(ctx, pool, testTenantID, "backend/testdata/locations/sheds-db-profile-coverage-sample.json", status, blocker); err != nil {
		t.Fatalf("upsertCSG7Readiness: %v", err)
	}
	var gotStatus, gotEvidence string
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref
FROM counts_shifting_readiness_subgates
WHERE tenant_id = $1::uuid AND subgate_id = 'CSG7'`, testTenantID).Scan(&gotStatus, &gotEvidence); err != nil {
		t.Fatalf("readiness row: %v", err)
	}
	if gotStatus != "pending" || !strings.Contains(gotEvidence, "location-profile-coverage-check") {
		t.Fatalf("readiness status=%s evidence=%s, want pending coverage evidence", gotStatus, gotEvidence)
	}
}

type fakeProfileDB struct {
	aliases    map[string]bool
	capacities map[string]bool
	err        error
}

func (db *fakeProfileDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "FROM location_aliases") {
		locationID, _ := args[1].(string)
		aliasNorm, _ := args[2].(string)
		sourceContext, _ := args[3].(string)
		return fakeBoolRow{covered: db.aliases[locationID+"\x00"+sourceContext+"\x00"+aliasNorm], err: db.err}
	}
	locationID, _ := args[1].(string)
	capacityKind, _ := args[2].(string)
	capacityValue, _ := args[3].(int)
	effectiveFrom, _ := args[4].(string)
	source, _ := args[5].(string)
	key := locationID + "\x00" + capacityKind + "\x00" + fmtInt(capacityValue) + "\x00" + effectiveFrom + "\x00" + source
	return fakeBoolRow{covered: db.capacities[key], err: db.err}
}

type fakeBoolRow struct {
	covered bool
	err     error
}

func (row fakeBoolRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	ok, _ := dest[0].(*bool)
	*ok = row.covered
	return nil
}

type fakeReadinessDB struct {
	query string
	args  []any
	err   error
}

func (db *fakeReadinessDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.query = query
	db.args = args
	return pgconn.CommandTag{}, db.err
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}
