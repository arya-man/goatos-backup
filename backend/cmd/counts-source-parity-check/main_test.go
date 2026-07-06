package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

var parityNow = time.Date(2026, 6, 30, 13, 30, 0, 0, time.UTC)

const (
	parityTenant = "00000000-0000-4000-8000-000000000001"
	parityPark   = "62000000-0000-4000-8000-000000000001"
	parityShed   = "62000000-0000-4000-8000-000000000002"
)

func TestParseParityFileNormalizesRows(t *testing.T) {
	fixture, err := parseParityFile(strings.NewReader(`{
		"tenant_id": "tenant-1",
		"source_ref": "context/source-findings/feed-direction-counting-db-reconstruction.md",
		"horizon": "feed_target_date",
		"park_id": "park-1",
		"target_date": "2026-07-01T15:00:00Z",
		"coverage_mode": "exact",
		"expected_rows": [
			{"shed_id": "shed-2", "breed_key": "Sirohi", "stage_tag": "Late Gestation", "age_class": "Adult Doe", "sex": "Female", "head_count": 3, "pregnant_count": 3},
			{"shed_id": "shed-1", "breed_key": "Beetal", "head_count": 10, "ration_context_resolution_state": "resolved"}
		]
	}`), config{}, func() time.Time { return parityNow })
	if err != nil {
		t.Fatalf("parseParityFile: %v", err)
	}
	if fixture.targetDate.Format(time.RFC3339) != "2026-07-01T00:00:00+05:30" {
		t.Fatalf("targetDate=%s", fixture.targetDate.Format(time.RFC3339))
	}
	if fixture.effectiveMode != modeExact {
		t.Fatalf("mode=%s", fixture.effectiveMode)
	}
	if fixture.ExpectedRows[0].ShedID != "shed-1" || fixture.ExpectedRows[0].BreedKey != "beetal" {
		t.Fatalf("first row=%+v, want sorted normalized row", fixture.ExpectedRows[0])
	}
	if fixture.ExpectedRows[1].StageTag == nil || *fixture.ExpectedRows[1].StageTag != "late_gestation" {
		t.Fatalf("stage tag=%v", fixture.ExpectedRows[1].StageTag)
	}
	if fixture.ExpectedRows[1].AgeClass == nil || *fixture.ExpectedRows[1].AgeClass != "adult_doe" {
		t.Fatalf("age class=%v", fixture.ExpectedRows[1].AgeClass)
	}
	if fixture.ExpectedRows[1].Sex == nil || *fixture.ExpectedRows[1].Sex != "female" {
		t.Fatalf("sex=%v", fixture.ExpectedRows[1].Sex)
	}
	if fixture.ExpectedRows[1].PregnantCount != 3 {
		t.Fatalf("pregnant_count=%d", fixture.ExpectedRows[1].PregnantCount)
	}
}

func TestParseParityFileRequiresTargetDateForProjectedHorizon(t *testing.T) {
	_, err := parseParityFile(strings.NewReader(`{
		"tenant_id": "tenant-1",
		"source_ref": "source.md",
		"horizon": "feed_target_date",
		"park_id": "park-1",
		"expected_rows": [{"shed_id": "shed-1", "breed_key": "beetal", "head_count": 1}]
	}`), config{}, func() time.Time { return parityNow })
	if err == nil || !strings.Contains(err.Error(), "target_date is required") {
		t.Fatalf("err=%v, want target_date requirement", err)
	}
}

func TestCompareParityRowsSampleModeIgnoresUnexpectedRows(t *testing.T) {
	state := "blocked"
	fixture := parityFile{
		effectiveMode: modeSample,
		ExpectedRows: []expectedParityRow{{
			ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10, RationContextResolutionState: &state,
		}},
	}
	projection := countsdomain.CountProjection{Rows: []countsdomain.ProjectionRow{
		{ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10, RationContextResolutionState: "blocked"},
		{ShedID: "shed-2", BreedKey: "sirohi", HeadCount: 4},
	}}
	result := compareParityRows(fixture, projection)
	if len(result.Missing) != 0 || len(result.Mismatched) != 0 || len(result.Unexpected) != 0 {
		t.Fatalf("result=%+v, want sample parity pass", result)
	}
}

func TestCompareParityRowsExactModeFlagsUnexpectedRows(t *testing.T) {
	fixture := parityFile{
		effectiveMode: modeExact,
		ExpectedRows:  []expectedParityRow{{ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10}},
	}
	projection := countsdomain.CountProjection{Rows: []countsdomain.ProjectionRow{
		{ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10},
		{ShedID: "shed-2", BreedKey: "sirohi", HeadCount: 4},
	}}
	result := compareParityRows(fixture, projection)
	if len(result.Unexpected) != 1 || !strings.Contains(result.Unexpected[0], "shed-2") {
		t.Fatalf("unexpected=%v, want shed-2", result.Unexpected)
	}
}

func TestCompareParityRowsFindsMissingAndMismatchedRows(t *testing.T) {
	state := "resolved"
	fixture := parityFile{
		effectiveMode: modeExact,
		ExpectedRows: []expectedParityRow{
			{ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10, RationContextResolutionState: &state},
			{ShedID: "shed-2", BreedKey: "sirohi", HeadCount: 4},
		},
	}
	projection := countsdomain.CountProjection{Rows: []countsdomain.ProjectionRow{{
		ShedID: "shed-1", BreedKey: "beetal", HeadCount: 9, RationContextResolutionState: "blocked",
	}}}
	result := compareParityRows(fixture, projection)
	if len(result.Missing) != 1 || !strings.Contains(result.Missing[0], "shed-2") {
		t.Fatalf("missing=%v, want shed-2", result.Missing)
	}
	if len(result.Mismatched) != 2 {
		t.Fatalf("mismatched=%v, want head_count and ration_state mismatches", result.Mismatched)
	}
}

func TestCompareParityRowsChecksHighRiskCohortDimensions(t *testing.T) {
	fixture, err := parseParityFile(strings.NewReader(`{
		"tenant_id": "tenant-1",
		"source_ref": "source.md",
		"horizon": "feed_target_date",
		"park_id": "park-1",
		"target_date": "2026-07-01",
		"expected_rows": [{
			"shed_id": "shed-1",
			"breed_key": "Beetal",
			"stage_tag": "Pregnant",
			"age_class": "Adult Doe",
			"sex": "Female",
			"head_count": 8,
			"pregnant_count": 3,
			"lactating_count": 1,
			"warmup_count": 2,
			"ration_context_resolution_state": "blocked"
		}]
	}`), config{}, func() time.Time { return parityNow })
	if err != nil {
		t.Fatalf("parseParityFile: %v", err)
	}
	actualStage := "pregnant"
	actualAge := "adult_doe"
	actualSex := "female"
	projection := countsdomain.CountProjection{Rows: []countsdomain.ProjectionRow{{
		ShedID: "shed-1", BreedKey: "beetal", StageTag: &actualStage, AgeClass: &actualAge, Sex: &actualSex,
		HeadCount: 8, PregnantCount: 2, LactatingCount: 0, WarmupCount: 1,
		RationContextResolutionState: "blocked",
	}}}
	result := compareParityRows(fixture, projection)
	if len(result.Missing) != 0 {
		t.Fatalf("missing=%v, want same shed/breed/stage/age/sex key", result.Missing)
	}
	if len(result.Mismatched) != 3 {
		t.Fatalf("mismatched=%v, want pregnant/lactating/warmup mismatches", result.Mismatched)
	}
	for _, field := range []string{"pregnant_count", "lactating_count", "warmup_count"} {
		found := false
		for _, mismatch := range result.Mismatched {
			found = found || strings.Contains(mismatch, field)
		}
		if !found {
			t.Fatalf("mismatched=%v, want %s mismatch", result.Mismatched, field)
		}
	}
}

func TestParseParityFileRejectsHighRiskCountsAboveHeadCount(t *testing.T) {
	_, err := parseParityFile(strings.NewReader(`{
		"tenant_id": "tenant-1",
		"source_ref": "source.md",
		"horizon": "feed_target_date",
		"park_id": "park-1",
		"target_date": "2026-07-01",
		"expected_rows": [{"shed_id": "shed-1", "breed_key": "beetal", "head_count": 2, "pregnant_count": 3}]
	}`), config{}, func() time.Time { return parityNow })
	if err == nil || !strings.Contains(err.Error(), "must not exceed head_count") {
		t.Fatalf("err=%v, want high-risk count validation", err)
	}
}

func TestParseHighRiskSourceParityFixtureCoversCriticalCohorts(t *testing.T) {
	fixture := loadHighRiskParityFixture(t)
	if fixture.TenantID != parityTenant || fixture.ParkID != parityPark {
		t.Fatalf("tenant=%s park=%s, want committed parity scope", fixture.TenantID, fixture.ParkID)
	}
	if len(fixture.ExpectedRows) != 5 {
		t.Fatalf("rows=%d, want five high-risk/sample rows", len(fixture.ExpectedRows))
	}
	required := map[string]bool{
		"pregnant_late_gestation": false,
		"mother_milking_waiting":  false,
		"warmup_pregnant":         false,
		"fattening_male_warmup":   false,
		"non_pregnant":            false,
	}
	var pregnant, lactating, warmup bool
	for _, row := range fixture.ExpectedRows {
		stage := ptrValue(row.StageTag)
		if _, ok := required[stage]; ok {
			required[stage] = true
		}
		pregnant = pregnant || row.PregnantCount > 0
		lactating = lactating || row.LactatingCount > 0
		warmup = warmup || row.WarmupCount > 0
	}
	for stage, found := range required {
		if !found {
			t.Fatalf("missing stage %s in high-risk source parity fixture", stage)
		}
	}
	if !pregnant || !lactating || !warmup {
		t.Fatalf("pregnant=%v lactating=%v warmup=%v, want all critical counters covered", pregnant, lactating, warmup)
	}
}

func TestCheckSourceParityReadsRequestedHorizon(t *testing.T) {
	reader := &fakeProjectionReader{projected: countsdomain.CountProjection{Rows: []countsdomain.ProjectionRow{{
		ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10,
	}}}}
	fixture := parityFile{
		TenantID: "tenant-1", Horizon: horizonFeedTargetDate, ParkID: "park-1",
		targetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		asOf:       parityNow, effectiveMode: modeSample,
		ExpectedRows: []expectedParityRow{{ShedID: "shed-1", BreedKey: "beetal", HeadCount: 10}},
	}
	result, err := checkSourceParity(context.Background(), reader, fixture, 25)
	if err != nil {
		t.Fatalf("checkSourceParity: %v", err)
	}
	if reader.projectedCalls != 1 || reader.countAsOfCalls != 0 {
		t.Fatalf("projectedCalls=%d countAsOfCalls=%d", reader.projectedCalls, reader.countAsOfCalls)
	}
	if result.Expected != 1 || result.Actual != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestParityStatusKeepsPassingCheckPending(t *testing.T) {
	status, blocker := parityStatus("source.md", parityResult{Expected: 2, Actual: 2})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestParityStatusBlocksOnFailures(t *testing.T) {
	status, blocker := parityStatus("source.md", parityResult{
		Expected: 2, Actual: 1, Missing: []string{"shed-2\x00sirohi\x00"}, Truncated: true,
	})
	if status != "blocked" {
		t.Fatalf("status=%s, want blocked", status)
	}
	if !strings.Contains(blocker, "next_cursor") || !strings.Contains(blocker, "missing=") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestUpsertCSG10ReadinessWritesParityEvidence(t *testing.T) {
	db := &fakeReadinessDB{}
	err := upsertCSG10Readiness(context.Background(), db, "tenant-1", "source.md", "pending", "still pending")
	if err != nil {
		t.Fatalf("upsertCSG10Readiness: %v", err)
	}
	if !strings.Contains(db.query, "counts-source-parity-check") {
		t.Fatalf("query=%q, want command implementation ref", db.query)
	}
	if db.args[1] != "pending" {
		t.Fatalf("status arg=%v", db.args[1])
	}
	evidence, _ := db.args[2].(string)
	if !strings.HasPrefix(evidence, "counts-source-parity-check:source.md:") {
		t.Fatalf("evidence=%q", evidence)
	}
}

func TestSourceParityAgainstMigratedProjectionUpdatesCSG10(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fixture := loadHighRiskParityFixture(t)
	seedParityScope(t, ctx, pool, parityShedsFromFixture(fixture)...)

	repo := countspg.NewRepository(pool, 5*time.Second)
	rows := projectionRowsForFixture(t, ctx, repo, fixture)
	snapshotID, err := repo.CreateProjectionSnapshot(ctx, countsdomain.ProjectionSnapshot{
		TenantID: fixture.TenantID, Horizon: fixture.Horizon, ParkID: fixture.ParkID,
		TargetDate: fixture.targetDate, AsOf: fixture.asOf, ProjectionStatus: "blocked",
		SourceContractVersion: countsdomain.SourceContractVersionV1,
		SourceHash:            "source-parity-high-risk-snapshot-hash",
		BaseAnchorIDsHash:     "source-parity-high-risk-anchor-hash",
		ShiftingEventIDsHash:  "source-parity-high-risk-shift-hash",
		GeneratedBy:           "counts-source-parity-check-test",
		Rows:                  rows,
	})
	if err != nil || snapshotID == "" {
		t.Fatalf("CreateProjectionSnapshot snapshot=%q err=%v", snapshotID, err)
	}

	result, err := checkSourceParity(ctx, repo, fixture, 25)
	if err != nil {
		t.Fatalf("checkSourceParity: %v", err)
	}
	if result.Expected != len(fixture.ExpectedRows) || result.Actual != len(fixture.ExpectedRows) ||
		len(result.Missing) != 0 || len(result.Mismatched) != 0 || len(result.Unexpected) != 0 || result.Truncated {
		t.Fatalf("result=%+v, want migrated multi-cohort projection parity pass", result)
	}
	status, blocker := parityStatus(fixture.SourceRef, result)
	if status != "pending" || !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("status=%s blocker=%q, want pending caveat", status, blocker)
	}
	if err := upsertCSG10Readiness(ctx, pool, parityTenant, fixture.SourceRef, status, blocker); err != nil {
		t.Fatalf("upsertCSG10Readiness: %v", err)
	}
	var gotStatus, gotEvidence, gotBlocker string
	if err := pool.QueryRow(ctx, `
SELECT status, evidence_ref, blocker_reason
FROM counts_shifting_readiness_subgates
WHERE tenant_id=$1::uuid AND subgate_id='CSG10'`, parityTenant).Scan(&gotStatus, &gotEvidence, &gotBlocker); err != nil {
		t.Fatalf("load CSG10 readiness: %v", err)
	}
	if gotStatus != "pending" || !strings.Contains(gotEvidence, "counts-source-parity-check") || !strings.Contains(gotBlocker, "seeded local E2E") {
		t.Fatalf("CSG10 status=%s evidence=%s blocker=%q, want pending source parity evidence", gotStatus, gotEvidence, gotBlocker)
	}
}

func loadHighRiskParityFixture(t *testing.T) parityFile {
	t.Helper()
	reader, closeFn, err := inputReader("../../testdata/counts/source-parity-high-risk-sample.json")
	if err != nil {
		t.Fatalf("open high-risk parity fixture: %v", err)
	}
	defer closeFn()
	fixture, err := parseParityFile(reader, config{}, func() time.Time { return parityNow })
	if err != nil {
		t.Fatalf("parse high-risk parity fixture: %v", err)
	}
	return fixture
}

func projectionRowsForFixture(t *testing.T, ctx context.Context, repo *countspg.Repository, fixture parityFile) []countsdomain.ProjectionRow {
	t.Helper()
	rows := make([]countsdomain.ProjectionRow, 0, len(fixture.ExpectedRows))
	for _, expected := range fixture.ExpectedRows {
		anchorID, replay, err := repo.RecordBaseCountAnchor(ctx, countsdomain.BaseCountAnchor{
			TenantID: fixture.TenantID, ParkID: fixture.ParkID, ShedID: expected.ShedID,
			BreedKey: expected.BreedKey, BreedLabel: breedLabelFor(expected.BreedKey),
			CountedAt:          time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC),
			HeadCount:          expected.HeadCount,
			SourceSystem:       "physical_base_count",
			SourceRef:          "source-parity-high-risk-base-count",
			SourceHash:         "source-parity-high-risk-base-hash-" + expected.ShedID,
			DiscrepancyState:   "not_checked",
			IdempotencyKey:     "source-parity-high-risk-base-anchor-" + expected.ShedID,
			RequestFingerprint: "source-parity-high-risk-base-fp-" + expected.ShedID,
		})
		if err != nil || replay || anchorID == "" {
			t.Fatalf("RecordBaseCountAnchor shed=%s anchor=%q replay=%v err=%v", expected.ShedID, anchorID, replay, err)
		}
		blocker := ""
		if expected.RationContextResolutionState != nil && *expected.RationContextResolutionState == "blocked" {
			blocker = "source parity sample blocks until destination shed ration context is reviewed"
		}
		rows = append(rows, countsdomain.ProjectionRow{
			ParkID: fixture.ParkID, ShedID: expected.ShedID, TargetDate: fixture.targetDate,
			GrainKey:                     "source-parity-high-risk:" + expected.ShedID + ":" + expected.BreedKey + ":" + ptrValue(expected.StageTag),
			BaseCountAnchorID:            anchorID,
			IncludedShiftingEventIDsHash: "source-parity-high-risk-shift-hash",
			BreedKey:                     expected.BreedKey,
			BreedLabel:                   breedLabelFor(expected.BreedKey),
			StageTag:                     expected.StageTag,
			AgeClass:                     expected.AgeClass,
			Sex:                          expected.Sex,
			HeadCount:                    expected.HeadCount,
			PregnantCount:                expected.PregnantCount,
			LactatingCount:               expected.LactatingCount,
			WarmupCount:                  expected.WarmupCount,
			RationContextResolutionState: ptrValue(expected.RationContextResolutionState),
			BlockerReason:                ptrIfNotEmpty(blocker),
			SourceRowHash:                "source-parity-high-risk-row-hash-" + expected.ShedID,
		})
	}
	return rows
}

func parityShedsFromFixture(fixture parityFile) []string {
	seen := map[string]bool{}
	var sheds []string
	for _, row := range fixture.ExpectedRows {
		if !seen[row.ShedID] {
			seen[row.ShedID] = true
			sheds = append(sheds, row.ShedID)
		}
	}
	return sheds
}

func breedLabelFor(key string) string {
	switch key {
	case "beetal":
		return "Beetal"
	case "sirohi":
		return "Sirohi"
	case "osmanabadi":
		return "Osmanabadi"
	case "boer":
		return "Boer"
	default:
		return key
	}
}

func ptrIfNotEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func seedParityScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedIDs ...string) {
	t.Helper()
	if len(shedIDs) == 0 {
		shedIDs = []string{parityShed}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'PARITY-PARK', 'Parity Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, parityTenant, parityPark); err != nil {
		t.Fatalf("seed parity park: %v", err)
	}
	for _, shedID := range shedIDs {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'PARITY-SHED-' || right($3::text, 2), 'Parity Shed ' || right($3::text, 2), $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, parityTenant, parityPark, shedID); err != nil {
			t.Fatalf("seed parity shed %s: %v", shedID, err)
		}
	}
}

type fakeProjectionReader struct {
	countAsOf      countsdomain.CountProjection
	projected      countsdomain.CountProjection
	countAsOfCalls int
	projectedCalls int
}

func (f *fakeProjectionReader) CountAsOf(context.Context, countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error) {
	f.countAsOfCalls++
	return f.countAsOf, nil
}

func (f *fakeProjectionReader) ProjectedCountFor(context.Context, countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error) {
	f.projectedCalls++
	return f.projected, nil
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
