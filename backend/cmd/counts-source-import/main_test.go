package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
)

func TestParseBaseCountAnchorDerivesSourceHashAndIdempotency(t *testing.T) {
	input := strings.NewReader(`{"kind":"base_count_anchor","park_id":"00000000-0000-4000-8000-000000000010","shed_id":"00000000-0000-4000-8000-000000000011","breed_key":"Beetal","counted_at":"2026-06-30","head_count":42,"source_ref":"Counting DB - values only.xlsx:Base Count!A2"}` + "\n")

	rows, err := parseImportRows(input, config{
		TenantID:     "00000000-0000-4000-8000-000000000001",
		SourceSystem: defaultSourceSystem,
	})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != kindBaseCountAnchor {
		t.Fatalf("kind=%s, want base_count_anchor", row.Kind)
	}
	if !row.DerivedSourceHash || !row.DerivedIdem || !row.DerivedFingerprint {
		t.Fatalf("derived flags source=%t idem=%t fingerprint=%t, want all true", row.DerivedSourceHash, row.DerivedIdem, row.DerivedFingerprint)
	}
	anchor := row.BaseCountAnchor
	if anchor.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenant=%s", anchor.TenantID)
	}
	if anchor.BreedLabel != "Beetal" {
		t.Fatalf("breed_label=%s, want fallback to breed key", anchor.BreedLabel)
	}
	if got := anchor.CountedAt.Format(time.RFC3339); got != "2026-06-30T00:00:00Z" {
		t.Fatalf("counted_at=%s", got)
	}
	if !strings.HasPrefix(anchor.IdempotencyKey, "counts-base-anchor-import:") {
		t.Fatalf("idempotency_key=%s", anchor.IdempotencyKey)
	}
	if anchor.SourceHash == "" || anchor.RequestFingerprint == "" {
		t.Fatalf("source_hash/request_fingerprint must be derived")
	}
}

func TestParseShiftingEventInfersPregnancyCategoryAndLeavesRationUnresolved(t *testing.T) {
	input := strings.NewReader(`{"kind":"shifting_event","tenant_id":"00000000-0000-4000-8000-000000000001","logical_shifting_event_key":"shift-docx-2026-06-30-1","source_park_id":"00000000-0000-4000-8000-000000000010","source_shed_id":"00000000-0000-4000-8000-000000000011","destination_park_id":"00000000-0000-4000-8000-000000000010","destination_shed_id":"00000000-0000-4000-8000-000000000012","raised_at":"2026-06-30T12:30:00+05:30","effective_at":"2026-06-30T14:00:00+05:30","authorization_state":"authorized","event_status":"authorized","source_ref":"Feed, Shiftings and Count.docx:shift-1","impacts":[{"breed_key":"Sirohi","stage_tag":"Late Gestation","head_count":3,"pregnant_count":3,"risk_flags":{"pregnancy_shift":true}}]}` + "\n")

	rows, err := parseImportRows(input, config{SourceSystem: defaultSourceSystem})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	event := rows[0].ShiftingEvent
	if event.Category != "pregnancy" {
		t.Fatalf("category=%s, want pregnancy", event.Category)
	}
	if event.SourceSystem != defaultSourceSystem {
		t.Fatalf("source_system=%s", event.SourceSystem)
	}
	if got := event.EffectiveAt.UTC().Format(time.RFC3339); got != "2026-06-30T08:30:00Z" {
		t.Fatalf("effective_at=%s", got)
	}
	if len(event.Impacts) != 1 {
		t.Fatalf("impacts=%d, want 1", len(event.Impacts))
	}
	impact := event.Impacts[0]
	if impact.GrainKey != "00000000-0000-4000-8000-000000000012:sirohi" {
		t.Fatalf("grain_key=%s", impact.GrainKey)
	}
	if impact.BreedLabel != "Sirohi" {
		t.Fatalf("breed_label=%s, want fallback", impact.BreedLabel)
	}
	if impact.PregnantCount != 3 || impact.RationContextResolutionState != "" {
		t.Fatalf("pregnant=%d ration_state=%q, want pregnant and unresolved by repository default", impact.PregnantCount, impact.RationContextResolutionState)
	}
	if !rows[0].DerivedPayload || !rows[0].DerivedIdem || !rows[0].DerivedFingerprint {
		t.Fatalf("expected payload/idempotency/fingerprint to be derived")
	}
}

func TestParseShiftingEventRequiresExplicitLifecycleState(t *testing.T) {
	input := strings.NewReader(`{"kind":"shifting_event","tenant_id":"tenant","logical_shifting_event_key":"shift-1","destination_park_id":"park","destination_shed_id":"shed","raised_at":"2026-06-30T12:30:00Z","effective_at":"2026-06-30T14:00:00Z","source_ref":"row","impacts":[{"breed_key":"beetal","head_count":1}]}` + "\n")

	_, err := parseImportRows(input, config{SourceSystem: defaultSourceSystem})
	if err == nil || !strings.Contains(err.Error(), "authorization_state and event_status are required") {
		t.Fatalf("err=%v, want explicit lifecycle state requirement", err)
	}
}

func TestDryRunDoesNotNeedDatabase(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-dry-run"}, strings.NewReader(`{"kind":"base_count_anchor","tenant_id":"tenant","park_id":"park","shed_id":"shed","breed_key":"beetal","counted_at":"2026-06-30","head_count":1,"source_ref":"row"}`+"\n"), &out)
	if err != nil {
		t.Fatalf("run dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry-run ok rows=1") {
		t.Fatalf("output=%s", out.String())
	}
}

type fakeCountsService struct {
	baseIDs  []string
	shiftIDs []string
}

func (f *fakeCountsService) RecordBaseCountAnchor(_ context.Context, in countsdomain.BaseCountAnchor) (string, bool, error) {
	f.baseIDs = append(f.baseIDs, in.IdempotencyKey)
	return "base-id", false, nil
}

func (f *fakeCountsService) RecordShiftingEvent(_ context.Context, in countsdomain.ShiftingEvent) (string, bool, error) {
	f.shiftIDs = append(f.shiftIDs, in.IdempotencyKey)
	return "shift-id", true, nil
}

func TestImportRowsWritesBothKindsThroughService(t *testing.T) {
	rows, err := parseImportRows(strings.NewReader(strings.Join([]string{
		`{"kind":"base_count_anchor","tenant_id":"tenant","park_id":"park","shed_id":"shed","breed_key":"beetal","counted_at":"2026-06-30","head_count":1,"source_ref":"row"}`,
		`{"kind":"shifting_event","tenant_id":"tenant","logical_shifting_event_key":"shift-1","destination_park_id":"park","destination_shed_id":"shed","raised_at":"2026-06-30T12:30:00Z","effective_at":"2026-06-30T14:00:00Z","authorization_state":"authorized","event_status":"authorized","source_ref":"row","impacts":[{"breed_key":"beetal","head_count":1}]}`,
	}, "\n")+"\n"), config{SourceSystem: defaultSourceSystem})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	var out bytes.Buffer
	service := &fakeCountsService{}
	summary, err := importRows(context.Background(), service, rows, &out)
	if err != nil {
		t.Fatalf("importRows: %v", err)
	}
	if summary.BaseAnchors != 1 || summary.ShiftingEvents != 1 || summary.Replayed != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(service.baseIDs) != 1 || len(service.shiftIDs) != 1 {
		t.Fatalf("service writes base=%d shift=%d", len(service.baseIDs), len(service.shiftIDs))
	}
}

type failingCountsService struct{}

func (f failingCountsService) RecordBaseCountAnchor(context.Context, countsdomain.BaseCountAnchor) (string, bool, error) {
	return "", false, errors.New("db rejected row")
}

func (f failingCountsService) RecordShiftingEvent(context.Context, countsdomain.ShiftingEvent) (string, bool, error) {
	return "", false, errors.New("db rejected row")
}

func TestImportRowsCountsFailedRow(t *testing.T) {
	rows, err := parseImportRows(strings.NewReader(`{"kind":"base_count_anchor","tenant_id":"tenant","park_id":"park","shed_id":"shed","breed_key":"beetal","counted_at":"2026-06-30","head_count":1,"source_ref":"row"}`+"\n"), config{SourceSystem: defaultSourceSystem})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	var out bytes.Buffer
	summary, err := importRows(context.Background(), failingCountsService{}, rows, &out)
	if err == nil || !strings.Contains(err.Error(), "db rejected row") {
		t.Fatalf("err=%v, want service error", err)
	}
	if summary.Rows != 1 || summary.FailedRows != 1 || summary.BaseAnchors != 0 {
		t.Fatalf("summary=%+v, want one failed row and no successful base anchors", summary)
	}
}

func TestSourceImportTenantIDDerivesFromRows(t *testing.T) {
	rows, err := parseImportRows(strings.NewReader(strings.Join([]string{
		`{"kind":"base_count_anchor","tenant_id":"00000000-0000-4000-8000-000000000001","park_id":"park","shed_id":"shed","breed_key":"beetal","counted_at":"2026-06-30","head_count":1,"source_ref":"row"}`,
		`{"kind":"shifting_event","tenant_id":"00000000-0000-4000-8000-000000000001","logical_shifting_event_key":"shift-1","destination_park_id":"park","destination_shed_id":"shed","raised_at":"2026-06-30T12:30:00Z","effective_at":"2026-06-30T14:00:00Z","authorization_state":"authorized","event_status":"authorized","source_ref":"row","impacts":[{"breed_key":"beetal","head_count":1}]}`,
	}, "\n")+"\n"), config{SourceSystem: defaultSourceSystem})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	tenantID, err := sourceImportTenantID(rows, "")
	if err != nil {
		t.Fatalf("sourceImportTenantID: %v", err)
	}
	if tenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenantID=%s", tenantID)
	}
}

func TestSourceImportTenantIDRejectsMixedRows(t *testing.T) {
	rows, err := parseImportRows(strings.NewReader(strings.Join([]string{
		`{"kind":"base_count_anchor","tenant_id":"00000000-0000-4000-8000-000000000001","park_id":"park","shed_id":"shed","breed_key":"beetal","counted_at":"2026-06-30","head_count":1,"source_ref":"row"}`,
		`{"kind":"shifting_event","tenant_id":"00000000-0000-4000-8000-000000000002","logical_shifting_event_key":"shift-1","destination_park_id":"park","destination_shed_id":"shed","raised_at":"2026-06-30T12:30:00Z","effective_at":"2026-06-30T14:00:00Z","authorization_state":"authorized","event_status":"authorized","source_ref":"row","impacts":[{"breed_key":"beetal","head_count":1}]}`,
	}, "\n")+"\n"), config{SourceSystem: defaultSourceSystem})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	_, err = sourceImportTenantID(rows, "")
	if err == nil || !strings.Contains(err.Error(), "one tenant") {
		t.Fatalf("err=%v, want mixed tenant rejection", err)
	}
}

type fakeImportRunDB struct {
	rowID string
	execs []fakeExec
}

type fakeExec struct {
	query string
	args  []any
}

func (f *fakeImportRunDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeImportRunRow{id: f.rowID}
}

func (f *fakeImportRunDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, fakeExec{query: query, args: args})
	return pgconn.CommandTag{}, nil
}

type fakeImportRunRow struct {
	id string
}

func (r fakeImportRunRow) Scan(dest ...any) error {
	*(dest[0].(*string)) = r.id
	return nil
}

func TestSourceImportRunEvidenceUpdatesCSG10Pending(t *testing.T) {
	db := &fakeImportRunDB{rowID: "00000000-0000-4000-8000-000000001234"}
	cfg := config{
		TenantID:     "00000000-0000-4000-8000-000000000001",
		SourceSystem: defaultSourceSystem,
		SourceRef:    "Counting DB - values only.xlsx:reviewed-jsonl",
		TraceID:      "trace-1",
	}
	runID, err := beginSourceImportRun(context.Background(), db, cfg, 2)
	if err != nil {
		t.Fatalf("beginSourceImportRun: %v", err)
	}
	if runID != db.rowID {
		t.Fatalf("runID=%s", runID)
	}
	err = finishSourceImportRun(context.Background(), db, cfg.TenantID, runID, importSummary{
		Rows: 2, BaseAnchors: 1, ShiftingEvents: 1, Replayed: 1,
	}, nil)
	if err != nil {
		t.Fatalf("finishSourceImportRun: %v", err)
	}
	if len(db.execs) != 3 {
		t.Fatalf("exec count=%d, want finish + CSG10 + CSG8 readiness", len(db.execs))
	}
	readinessArgs := db.execs[1].args
	if readinessArgs[1] != "pending" {
		t.Fatalf("readiness status=%v, want pending", readinessArgs[1])
	}
	if !strings.Contains(readinessArgs[2].(string), runID) {
		t.Fatalf("evidence_ref=%v, want run id", readinessArgs[2])
	}
	if !strings.Contains(readinessArgs[3].(string), "seeded local E2E") {
		t.Fatalf("blocker=%v", readinessArgs[3])
	}
	replayArgs := db.execs[2].args
	if !strings.Contains(replayArgs[1].(string), runID) {
		t.Fatalf("CSG8 evidence_ref=%v, want run id", replayArgs[1])
	}
}

func TestSourceImportRunEvidenceBlocksOnFailure(t *testing.T) {
	db := &fakeImportRunDB{rowID: "00000000-0000-4000-8000-000000001235"}
	err := finishSourceImportRun(context.Background(), db, "00000000-0000-4000-8000-000000000001", db.rowID, importSummary{}, errors.New("bad source row"))
	if err != nil {
		t.Fatalf("finishSourceImportRun: %v", err)
	}
	readinessArgs := db.execs[1].args
	if readinessArgs[1] != "blocked" {
		t.Fatalf("readiness status=%v, want blocked", readinessArgs[1])
	}
	if !strings.Contains(readinessArgs[3].(string), "bad source row") {
		t.Fatalf("blocker=%v, want error text", readinessArgs[3])
	}
}
