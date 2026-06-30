package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
)

var parityNow = time.Date(2026, 6, 30, 13, 30, 0, 0, time.UTC)

func TestParseParityFileNormalizesRows(t *testing.T) {
	fixture, err := parseParityFile(strings.NewReader(`{
		"tenant_id": "tenant-1",
		"source_ref": "context/source-findings/feed-direction-counting-db-reconstruction.md",
		"horizon": "feed_target_date",
		"park_id": "park-1",
		"target_date": "2026-07-01T15:00:00Z",
		"coverage_mode": "exact",
		"expected_rows": [
			{"shed_id": "shed-2", "breed_key": "Sirohi", "stage_tag": "Late Gestation", "head_count": 3},
			{"shed_id": "shed-1", "breed_key": "Beetal", "head_count": 10, "ration_context_resolution_state": "resolved"}
		]
	}`), config{}, func() time.Time { return parityNow })
	if err != nil {
		t.Fatalf("parseParityFile: %v", err)
	}
	if fixture.targetDate.Format(time.RFC3339) != "2026-07-01T00:00:00Z" {
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
