package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	secondTenant         = "00000000-0000-4000-8000-000000000002"
	meshaParty           = "00000000-0000-4000-8000-000000001001"
	cbeLocation          = "00000000-0000-4000-8000-000000003001"
	cptLocation          = "00000000-0000-4000-8000-000000003002"
	t2Location           = "00000000-0000-4000-8000-000000003101"
	importRunID          = "30000000-0000-4000-8000-000000000001"
	secondTenantRunID    = "30000000-0000-4000-8000-000000000002"
)

func TestRepositoryReadPathsWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	container := fmt.Sprintf("goatos-repo-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
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
	seedRepositoryData(t, container)

	pool := openPool(t, ctx, container)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	passport, err := repo.GetGoatByID(ctx, meshaTenant, "10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("GetGoatByID: %v", err)
	}
	if passport.DisplayID == "" || passport.Summary.PrimaryOldTag == nil || *passport.Summary.PrimaryOldTag != "1900" {
		t.Fatalf("unexpected passport: %#v", passport)
	}
	if len(passport.Identifiers) != 1 || passport.Identifiers[0].IdentifierValue != "1900" {
		t.Fatalf("unexpected passport identifiers: %#v", passport.Identifiers)
	}

	displayPassport, err := repo.GetGoatByDisplayID(ctx, meshaTenant, passport.DisplayID)
	if err != nil {
		t.Fatalf("GetGoatByDisplayID: %v", err)
	}
	if displayPassport.GoatID != passport.GoatID || displayPassport.DisplayID != passport.DisplayID {
		t.Fatalf("display lookup returned wrong goat: %#v", displayPassport)
	}
	if len(displayPassport.Identifiers) != 1 || displayPassport.Identifiers[0].ScopeKey != "park:CBE" {
		t.Fatalf("display lookup identifiers wrong: %#v", displayPassport.Identifiers)
	}

	if _, err := repo.GetGoatByID(ctx, secondTenant, "10000000-0000-4000-8000-000000000001"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-tenant goat lookup should return ErrNotFound, got %v", err)
	}
	if _, err := repo.GetGoatByDisplayID(ctx, secondTenant, passport.DisplayID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-tenant display lookup should return ErrNotFound, got %v", err)
	}

	matches, err := repo.FindIdentifierMatches(ctx, ports.ResolveIdentifierParams{
		TenantID:        meshaTenant,
		IdentifierType:  "old_tag",
		NormalizedValue: "1900",
		ScopeKey:        strPtr("park:CBE"),
	})
	if err != nil {
		t.Fatalf("FindIdentifierMatches scoped: %v", err)
	}
	if len(matches) != 1 || matches[0].Goat.GoatID != "10000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected scoped matches: %#v", matches)
	}

	matches, err = repo.FindIdentifierMatches(ctx, ports.ResolveIdentifierParams{
		TenantID:        meshaTenant,
		IdentifierType:  "old_tag",
		NormalizedValue: "1900",
	})
	if err != nil {
		t.Fatalf("FindIdentifierMatches all scopes: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("same old tag across scopes should return 2 visible matches, got %d", len(matches))
	}

	conflict, err := repo.FindOpenConflictForIdentifier(ctx, meshaTenant, "old_tag", "1900", "park:CBE")
	if err != nil {
		t.Fatalf("FindOpenConflictForIdentifier: %v", err)
	}
	if conflict == nil || *conflict != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected conflict id: %v", conflict)
	}
	conflict, err = repo.FindOpenConflictForIdentifier(ctx, meshaTenant, "old_tag", "1900", "park:CPT")
	if err != nil {
		t.Fatalf("FindOpenConflictForIdentifier wrong scope: %v", err)
	}
	if conflict != nil {
		t.Fatalf("unexpected cross-scope conflict id: %v", *conflict)
	}

	detail, err := repo.GetConflict(ctx, meshaTenant, "20000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("GetConflict: %v", err)
	}
	if detail.Conflict.ConflictID != "20000000-0000-4000-8000-000000000001" ||
		detail.Conflict.GoatCount != 2 ||
		detail.Conflict.SourceRecordCount != 1 ||
		detail.Conflict.Identifier == nil ||
		detail.Conflict.Identifier.ScopeKey != "park:CBE" {
		t.Fatalf("unexpected conflict summary: %#v", detail.Conflict)
	}
	if len(detail.Goats) != 2 {
		t.Fatalf("expected 2 conflict goats, got %#v", detail.Goats)
	}
	if detail.Goats[0].Goat.GoatID != "10000000-0000-4000-8000-000000000001" ||
		detail.Goats[1].Goat.GoatID != "10000000-0000-4000-8000-000000000002" {
		t.Fatalf("unexpected conflict goats: %#v", detail.Goats)
	}
	if len(detail.SourceRecords) != 1 ||
		detail.SourceRecords[0].SourceSystem != "synthetic_import" ||
		detail.SourceRecords[0].SourceRecordID != "synthetic-source-record-1" ||
		len(detail.SourceRecords[0].EvidenceRefs) != 1 {
		t.Fatalf("unexpected conflict source records: %#v", detail.SourceRecords)
	}
	if detail.LegacyEvidence == nil ||
		detail.LegacyEvidence.SourceContext != "bq_reconcile_attribute_conflict" ||
		len(detail.LegacyEvidence.Conflicts) != 1 ||
		detail.LegacyEvidence.Conflicts[0].Reason != "bq_gender_self_conflict" ||
		detail.LegacyEvidence.Conflicts[0].LegacyValue == nil ||
		*detail.LegacyEvidence.Conflicts[0].LegacyValue != "female|male" {
		t.Fatalf("unexpected legacy evidence: %#v", detail.LegacyEvidence)
	}
	if _, err := repo.GetConflict(ctx, secondTenant, "20000000-0000-4000-8000-000000000001"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-tenant conflict lookup should return ErrNotFound, got %v", err)
	}

	firstConflictPage, conflictCursor, err := repo.ListConflicts(ctx, ports.ListConflictsParams{TenantID: meshaTenant, Limit: 1})
	if err != nil {
		t.Fatalf("ListConflicts first page: %v", err)
	}
	if len(firstConflictPage) != 1 || conflictCursor == nil {
		t.Fatalf("unexpected first conflict page: items=%#v cursor=%v", firstConflictPage, conflictCursor)
	}
	secondConflictPage, nextConflictCursor, err := repo.ListConflicts(ctx, ports.ListConflictsParams{TenantID: meshaTenant, Limit: 10, Cursor: conflictCursor})
	if err != nil {
		t.Fatalf("ListConflicts second page: %v", err)
	}
	if len(secondConflictPage) != 1 || nextConflictCursor != nil {
		t.Fatalf("unexpected second conflict page: items=%#v cursor=%v", secondConflictPage, nextConflictCursor)
	}
	if secondConflictPage[0].ConflictID == firstConflictPage[0].ConflictID {
		t.Fatalf("conflict cursor duplicated first row: first=%s second=%s", firstConflictPage[0].ConflictID, secondConflictPage[0].ConflictID)
	}
	if secondConflictPage[0].ConflictID == "20000000-0000-4000-8000-000000000002" && secondConflictPage[0].SourceRecordCount != 1 {
		t.Fatalf("fallback source_record_ids should count as source records, got %#v", secondConflictPage[0])
	}
	fallbackDetail, err := repo.GetConflict(ctx, meshaTenant, "20000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatalf("GetConflict fallback source records: %v", err)
	}
	if fallbackDetail.Conflict.SourceRecordCount != 1 ||
		len(fallbackDetail.SourceRecords) != 1 ||
		fallbackDetail.SourceRecords[0].SourceSystem != "legacy_bigquery" ||
		fallbackDetail.SourceRecords[0].SourceRecordID != "old_tag:park:cpt:1901" {
		t.Fatalf("unexpected fallback source records: summary=%#v records=%#v", fallbackDetail.Conflict, fallbackDetail.SourceRecords)
	}
	if _, _, err := repo.ListConflicts(ctx, ports.ListConflictsParams{TenantID: meshaTenant, Limit: 10, Cursor: strPtr("not-a-valid-cursor")}); !errors.Is(err, ports.ErrInvalidCursor) {
		t.Fatalf("invalid conflict cursor should return ErrInvalidCursor, got %v", err)
	}

	runSummary, err := repo.GetImportRun(ctx, meshaTenant, importRunID)
	if err != nil {
		t.Fatalf("GetImportRun: %v", err)
	}
	if runSummary.Summary.RowsProcessed != 3 ||
		runSummary.Summary.GoatsCreated != 1 ||
		runSummary.Summary.RowsNeedingReview != 2 ||
		runSummary.Summary.ErrorCount != 1 ||
		runSummary.Summary.CleanMatches != nil {
		t.Fatalf("unexpected import run summary: %#v", runSummary.Summary)
	}
	if _, err := repo.GetImportRun(ctx, secondTenant, importRunID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-tenant import run lookup should return ErrNotFound, got %v", err)
	}

	importRows, next, err := repo.ListImportRunRows(ctx, ports.ListImportRunRowsParams{TenantID: meshaTenant, ImportRunID: importRunID, Limit: 2})
	if err != nil {
		t.Fatalf("ListImportRunRows first page: %v", err)
	}
	if len(importRows) != 2 || next == nil || importRows[0].RowNumber != 1 || importRows[1].RowNumber != 2 {
		t.Fatalf("unexpected first import row page rows=%#v next=%v", importRows, next)
	}
	if importRows[1].SourceRowKeyRef == nil || !strings.HasPrefix(*importRows[1].SourceRowKeyRef, "sha256:") {
		t.Fatalf("expected source row key ref, got %#v", importRows[1].SourceRowKeyRef)
	}
	if len(importRows[1].ReviewReasons) != 1 || importRows[1].ReviewReasons[0] != "blank_old_tag_suffix" {
		t.Fatalf("expected de-duped reasons, got %#v", importRows[1].ReviewReasons)
	}
	importRows, next, err = repo.ListImportRunRows(ctx, ports.ListImportRunRowsParams{TenantID: meshaTenant, ImportRunID: importRunID, Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("ListImportRunRows second page: %v", err)
	}
	if len(importRows) != 1 || next != nil || importRows[0].RowNumber != 3 {
		t.Fatalf("unexpected second import row page rows=%#v next=%v", importRows, next)
	}

	state := "needs_review"
	importRows, _, err = repo.ListImportRunRows(ctx, ports.ListImportRunRowsParams{TenantID: meshaTenant, ImportRunID: importRunID, Limit: 10, ProcessingState: &state})
	if err != nil {
		t.Fatalf("ListImportRunRows state filter: %v", err)
	}
	if len(importRows) != 2 {
		t.Fatalf("expected 2 needs_review rows, got %#v", importRows)
	}

	reason := "blank_old_tag_suffix"
	importRows, _, err = repo.ListImportRunRows(ctx, ports.ListImportRunRowsParams{TenantID: meshaTenant, ImportRunID: importRunID, Limit: 10, ReasonCode: &reason})
	if err != nil {
		t.Fatalf("ListImportRunRows reason filter: %v", err)
	}
	if len(importRows) != 1 || importRows[0].RowNumber != 2 {
		t.Fatalf("expected only blank suffix row, got %#v", importRows)
	}
	if _, _, err := repo.ListImportRunRows(ctx, ports.ListImportRunRowsParams{TenantID: meshaTenant, ImportRunID: importRunID, Limit: 10, Cursor: strPtr("not-a-valid-cursor")}); !errors.Is(err, ports.ErrInvalidCursor) {
		t.Fatalf("invalid import row cursor should return ErrInvalidCursor, got %v", err)
	}

	timeline, nextTimeline, err := repo.GetGoatTimeline(ctx, ports.GetGoatTimelineParams{TenantID: meshaTenant, GoatID: "10000000-0000-4000-8000-000000000001", Limit: 1})
	if err != nil {
		t.Fatalf("GetGoatTimeline first page: %v", err)
	}
	if len(timeline) != 1 || timeline[0].EventType != "goat.identifier.added" || nextTimeline == nil {
		t.Fatalf("unexpected first timeline page rows=%#v next=%v", timeline, nextTimeline)
	}
	if len(timeline[0].EvidenceRefs) != 1 || timeline[0].EvidenceRefs[0].EvidenceID != "synthetic-row-2" {
		t.Fatalf("unexpected evidence refs: %#v", timeline[0].EvidenceRefs)
	}
	timeline, nextTimeline, err = repo.GetGoatTimeline(ctx, ports.GetGoatTimelineParams{TenantID: meshaTenant, GoatID: "10000000-0000-4000-8000-000000000001", Limit: 1, Cursor: nextTimeline})
	if err != nil {
		t.Fatalf("GetGoatTimeline second page: %v", err)
	}
	if len(timeline) != 1 || timeline[0].EventType != "goat.created" || nextTimeline != nil {
		t.Fatalf("unexpected second timeline page rows=%#v next=%v", timeline, nextTimeline)
	}
	if _, _, err := repo.GetGoatTimeline(ctx, ports.GetGoatTimelineParams{TenantID: meshaTenant, GoatID: "10000000-0000-4000-8000-000000000001", Limit: 10, Cursor: strPtr("not-a-valid-cursor")}); !errors.Is(err, ports.ErrInvalidCursor) {
		t.Fatalf("invalid timeline cursor should return ErrInvalidCursor, got %v", err)
	}
	timeline, _, err = repo.GetGoatTimeline(ctx, ports.GetGoatTimelineParams{TenantID: secondTenant, GoatID: "10000000-0000-4000-8000-000000000001", Limit: 10})
	if err != nil {
		t.Fatalf("GetGoatTimeline cross tenant: %v", err)
	}
	if len(timeline) != 0 {
		t.Fatalf("cross-tenant timeline should be empty, got %#v", timeline)
	}

	actorID := "90000000-0000-4000-8000-000000000001"
	corrections, nextCorrections, err := repo.ListCorrectionRequests(ctx, ports.ListCorrectionRequestsParams{TenantID: meshaTenant, CreatedBy: &actorID, Limit: 1})
	if err != nil {
		t.Fatalf("ListCorrectionRequests caller first page: %v", err)
	}
	if len(corrections) != 1 || corrections[0].CorrectionRequestID != "40000000-0000-4000-8000-000000000001" || nextCorrections == nil {
		t.Fatalf("unexpected caller correction page rows=%#v next=%v", corrections, nextCorrections)
	}
	corrections, nextCorrections, err = repo.ListCorrectionRequests(ctx, ports.ListCorrectionRequestsParams{TenantID: meshaTenant, CreatedBy: &actorID, Limit: 1, Cursor: nextCorrections})
	if err != nil {
		t.Fatalf("ListCorrectionRequests caller second page: %v", err)
	}
	if len(corrections) != 1 || corrections[0].CorrectionRequestID != "40000000-0000-4000-8000-000000000003" || nextCorrections != nil {
		t.Fatalf("unexpected caller correction second page rows=%#v next=%v", corrections, nextCorrections)
	}
	openState := "open"
	corrections, _, err = repo.ListCorrectionRequests(ctx, ports.ListCorrectionRequestsParams{TenantID: meshaTenant, State: &openState, Limit: 10})
	if err != nil {
		t.Fatalf("ListCorrectionRequests admin state: %v", err)
	}
	if len(corrections) != 1 || corrections[0].CorrectionRequestID != "40000000-0000-4000-8000-000000000001" || corrections[0].RowVersion == nil || *corrections[0].RowVersion != 1 {
		t.Fatalf("unexpected open corrections: %#v", corrections)
	}
	corrections, _, err = repo.ListCorrectionRequests(ctx, ports.ListCorrectionRequestsParams{TenantID: secondTenant, Limit: 10})
	if err != nil {
		t.Fatalf("ListCorrectionRequests cross tenant: %v", err)
	}
	if len(corrections) != 1 || corrections[0].CorrectionRequestID != "40000000-0000-4000-8000-000000000101" {
		t.Fatalf("unexpected second tenant corrections: %#v", corrections)
	}
	if _, _, err := repo.ListCorrectionRequests(ctx, ports.ListCorrectionRequestsParams{TenantID: meshaTenant, Limit: 10, Cursor: strPtr("not-a-valid-cursor")}); !errors.Is(err, ports.ErrInvalidCursor) {
		t.Fatalf("invalid correction cursor should return ErrInvalidCursor, got %v", err)
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
		upSQL := extractGooseUp(string(sqlBytes))
		psql(t, container, upSQL)
	}
}

func seedRepositoryData(t *testing.T, container string) {
	t.Helper()
	psql(t, container, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ('`+secondTenant+`', 'Synthetic second tenant', 'active');

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ('`+t2Location+`', '`+secondTenant+`', 'park', 'CBE', 'Synthetic tenant 2 CBE', 'active');

INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id, breed, sex)
VALUES
  ('10000000-0000-4000-8000-000000000001', '`+meshaTenant+`', 'alive', 'clean', '`+meshaParty+`', '`+cbeLocation+`', '`+cbeLocation+`', 'Synthetic Boer', 'female'),
  ('10000000-0000-4000-8000-000000000002', '`+meshaTenant+`', 'alive', 'clean', '`+meshaParty+`', '`+cptLocation+`', '`+cptLocation+`', 'Synthetic Boer', 'male'),
  ('10000000-0000-4000-8000-000000000101', '`+secondTenant+`', 'alive', 'clean', '`+meshaParty+`', '`+t2Location+`', '`+t2Location+`', 'Synthetic Boer', 'female');

INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000001', 'old_tag', '1900', '1900', 'park:CBE', true, 'active', now(), 'test_v1'),
  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000002', 'old_tag', '1900', '1900', 'park:CPT', true, 'active', now(), 'test_v1'),
  ('`+secondTenant+`', '10000000-0000-4000-8000-000000000101', 'old_tag', '1900', '1900', 'park:CBE', true, 'active', now(), 'test_v1');

INSERT INTO identity_conflicts (conflict_id, tenant_id, conflict_type, severity, state, identifier_type, identifier_value, goat_ids, source_record_ids, evidence)
VALUES (
  '20000000-0000-4000-8000-000000000001',
  '`+meshaTenant+`',
  'old_tag_reused',
  'medium',
  'open',
  'old_tag',
  '1900',
  ARRAY['10000000-0000-4000-8000-000000000001'::uuid, '10000000-0000-4000-8000-000000000002'::uuid],
  ARRAY['synthetic-source-record-1'],
  '{
    "scope_key":"park:CBE",
    "source_system":"legacy_bigquery",
    "source_context":"bq_reconcile_attribute_conflict",
    "review_note":"Synthetic legacy evidence for reviewer UI.",
    "latest_event_date":"2026-06-01",
    "latest_farm_code":"CBE",
    "matched_keys":["old_tag:park:cbe:1900"],
    "conflicts":[{
      "Attribute":"sex",
      "Reason":"bq_gender_self_conflict",
      "LocalValue":"female",
      "BQValue":"female|male",
      "IdentifierKind":"old_tag",
      "IdentifierKey":"old_tag:park:cbe:1900",
      "LatestEventDate":"2026-06-01",
      "RecommendedState":"Legacy gender has multiple values for this identifier; review source rows before changing canonical sex."
    }]
  }'::jsonb
);

INSERT INTO identity_conflict_goats (conflict_id, tenant_id, goat_id, role)
VALUES
  ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', '10000000-0000-4000-8000-000000000001', 'affected'),
  ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', '10000000-0000-4000-8000-000000000002', 'affected');

INSERT INTO identity_conflict_source_records (conflict_id, tenant_id, source_system, source_record_id)
VALUES ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', 'synthetic_import', 'synthetic-source-record-1');

INSERT INTO identity_conflicts (conflict_id, tenant_id, conflict_type, severity, state, identifier_type, identifier_value, goat_ids, source_record_ids, evidence)
VALUES (
  '20000000-0000-4000-8000-000000000002',
  '`+meshaTenant+`',
  'status_mismatch',
  'low',
  'open',
  'old_tag',
  '1901',
  ARRAY['10000000-0000-4000-8000-000000000002'::uuid],
  ARRAY['old_tag:park:cpt:1901'],
  '{"scope_key":"park:CPT","source_system":"legacy_bigquery"}'::jsonb
);

UPDATE identity_conflicts
SET created_at = '2026-06-10 00:00:00+00'
WHERE conflict_id IN (
  '20000000-0000-4000-8000-000000000001',
  '20000000-0000-4000-8000-000000000002'
);

INSERT INTO goat_identity_events (
  identity_event_id,
  tenant_id,
  goat_id,
  event_type,
  event_version,
  occurred_at,
  recorded_at,
  actor_id,
  source_system,
  source_record_id,
  payload,
  idempotency_key
)
VALUES
  (
    '60000000-0000-4000-8000-000000000001',
    '`+meshaTenant+`',
    '10000000-0000-4000-8000-000000000001',
    'goat.created',
    1,
    '2026-06-08 00:00:00+00',
    '2026-06-08 00:01:00+00',
    NULL,
    'synthetic_import',
    'synthetic-row-1',
    '{"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}]}'::jsonb,
    'idem-timeline-1'
  ),
  (
    '60000000-0000-4000-8000-000000000002',
    '`+meshaTenant+`',
    '10000000-0000-4000-8000-000000000001',
    'goat.identifier.added',
    1,
    '2026-06-09 00:00:00+00',
    '2026-06-09 00:01:00+00',
    '90000000-0000-4000-8000-000000000001',
    'synthetic_import',
    'synthetic-row-2',
    '{"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-2","source_system":"synthetic_import"}]}'::jsonb,
    'idem-timeline-2'
  ),
  (
    '60000000-0000-4000-8000-000000000101',
    '`+secondTenant+`',
    '10000000-0000-4000-8000-000000000101',
    'goat.created',
    1,
    '2026-06-08 00:00:00+00',
    '2026-06-08 00:01:00+00',
    NULL,
    'synthetic_import',
    'synthetic-row-tenant-2',
    '{"evidence_refs":[]}'::jsonb,
    'idem-timeline-tenant-2'
  );

INSERT INTO identity_correction_requests (
  correction_request_id,
  tenant_id,
  request_type,
  state,
  goat_id,
  identifier_type,
  identifier_value,
  park_id,
  description,
  evidence,
  requested_by,
  created_at,
  row_version
)
VALUES
  (
    '40000000-0000-4000-8000-000000000001',
    '`+meshaTenant+`',
    'missing_tag',
    'open',
    '10000000-0000-4000-8000-000000000001',
    'old_tag',
    '1900',
    '`+cbeLocation+`',
    'Synthetic open correction',
    '{"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-row-1","source_system":"synthetic_import"}]}'::jsonb,
    '90000000-0000-4000-8000-000000000001',
    '2026-06-10 00:00:00+00',
    1
  ),
  (
    '40000000-0000-4000-8000-000000000002',
    '`+meshaTenant+`',
    'wrong_status',
    'assigned',
    '10000000-0000-4000-8000-000000000002',
    NULL,
    NULL,
    '`+cptLocation+`',
    'Synthetic assigned correction',
    '{"evidence_refs":[]}'::jsonb,
    '90000000-0000-4000-8000-000000000002',
    '2026-06-09 00:00:00+00',
    1
  ),
  (
    '40000000-0000-4000-8000-000000000003',
    '`+meshaTenant+`',
    'field_verification_result',
    'closed',
    NULL,
    NULL,
    NULL,
    '`+cbeLocation+`',
    'Synthetic closed correction',
    '{"evidence_refs":[]}'::jsonb,
    '90000000-0000-4000-8000-000000000001',
    '2026-06-08 00:00:00+00',
    2
  ),
  (
    '40000000-0000-4000-8000-000000000101',
    '`+secondTenant+`',
    'missing_tag',
    'open',
    '10000000-0000-4000-8000-000000000101',
    'old_tag',
    '1900',
    '`+t2Location+`',
    'Synthetic second tenant correction',
    '{"evidence_refs":[]}'::jsonb,
    '90000000-0000-4000-8000-000000000001',
    '2026-06-10 00:00:00+00',
    1
  );

INSERT INTO legacy_import_runs (import_run_id, tenant_id, source_name, source_system, source_dataset, policy_version, dry_run, status, row_count, created_goat_count, error_count)
VALUES
  ('`+importRunID+`', '`+meshaTenant+`', 'Synthetic import', 'legacy_rfid_db', 'rfid_db_first_import', 'phase1-rfid-db-import-v1', false, 'completed', 3, 1, 1),
  ('`+secondTenantRunID+`', '`+secondTenant+`', 'Synthetic tenant 2 import', 'legacy_rfid_db', 'rfid_db_first_import', 'phase1-rfid-db-import-v1', false, 'completed', 1, 0, 0);

INSERT INTO legacy_import_rows (
  legacy_row_id,
  tenant_id,
  import_run_id,
  row_number,
  source_system,
  source_dataset,
  source_record_id,
  source_row_key,
  source_key_recipe_version,
  source_row_version_hash,
  hash_recipe_version,
  raw_payload,
  normalized_payload,
  processing_state,
  matched_goat_id,
  error_reason
)
VALUES
  (
    '70000000-0000-4000-8000-000000000001',
    '`+meshaTenant+`',
    '`+importRunID+`',
    1,
    'legacy_rfid_db',
    'rfid_db_first_import',
    'synthetic-row-1',
    'source-row-key-created-1',
    'phase1-rfid-source-key-v1',
    'sha256:created1',
    'phase1-rfid-row-hash-v1',
    '{"Tag":"F2","Breed":"Sirohi","Gender":"Female","RFID":"RFID-SYNTHETIC-001","Old ID":"1900","Farm":"Farm A","Shed":"Shed A","Partition":"P1"}'::jsonb,
    '{"tag":"F2","breed":"Sirohi","gender":"Female","rfid":"RFID-SYNTHETIC-001","normalized_old_tag":"1900","farm":"Farm A","shed":"Shed A","partition":"P1","processing_reasons":[]}'::jsonb,
    'created_goat',
    '10000000-0000-4000-8000-000000000001',
    NULL
  ),
  (
    '70000000-0000-4000-8000-000000000002',
    '`+meshaTenant+`',
    '`+importRunID+`',
    2,
    'legacy_rfid_db',
    'rfid_db_first_import',
    NULL,
    'source-row-key-review-2',
    'phase1-rfid-source-key-v1',
    'sha256:review2',
    'phase1-rfid-row-hash-v1',
    '{"Tag":"F2","Breed":"Sirohi","Gender":"Female","RFID":"RFID-SYNTHETIC-002","Old ID":"1901","Farm":"Farm A","Shed":"Shed B","Partition":"P2"}'::jsonb,
    '{"tag":"F2","breed":"Sirohi","gender":"Female","rfid":"RFID-SYNTHETIC-002","normalized_old_tag":"1901","farm":"Farm A","shed":"Shed B","partition":"P2","processing_reasons":["blank_old_tag_suffix","blank_old_tag_suffix"]}'::jsonb,
    'needs_review',
    NULL,
    'blank_old_tag_suffix'
  ),
  (
    '70000000-0000-4000-8000-000000000003',
    '`+meshaTenant+`',
    '`+importRunID+`',
    3,
    'legacy_rfid_db',
    'rfid_db_first_import',
    'synthetic-row-3',
    'source-row-key-review-3',
    'phase1-rfid-source-key-v1',
    'sha256:review3',
    'phase1-rfid-row-hash-v1',
    '{"Tag":"F2","Breed":"Anantapur Sheep","Gender":"Female","RFID":"RFID-SYNTHETIC-003","Old ID":"1902","Farm":"Farm B","Shed":"Shed C","Partition":"P3"}'::jsonb,
    '{"tag":"F2","breed":"Anantapur Sheep","gender":"Female","rfid":"RFID-SYNTHETIC-003","normalized_old_tag":"1902","farm":"Farm B","shed":"Shed C","partition":"P3","processing_reasons":["species_or_breed_requires_review"]}'::jsonb,
    'needs_review',
    NULL,
    NULL
  ),
  (
    '70000000-0000-4000-8000-000000000101',
    '`+secondTenant+`',
    '`+secondTenantRunID+`',
    1,
    'legacy_rfid_db',
    'rfid_db_first_import',
    'synthetic-row-tenant-2',
    'source-row-key-tenant-2',
    'phase1-rfid-source-key-v1',
    'sha256:tenant2',
    'phase1-rfid-row-hash-v1',
    '{"Tag":"F2","Breed":"Sirohi","Gender":"Female","RFID":"RFID-SYNTHETIC-101","Old ID":"1900","Farm":"Farm Z","Shed":"Shed Z","Partition":"P9"}'::jsonb,
    '{"tag":"F2","breed":"Sirohi","gender":"Female","rfid":"RFID-SYNTHETIC-101","normalized_old_tag":"1900","farm":"Farm Z","shed":"Shed Z","partition":"P9","processing_reasons":[]}'::jsonb,
    'created_goat',
    '10000000-0000-4000-8000-000000000101',
    NULL
  );

`)
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

func strPtr(value string) *string {
	return &value
}
