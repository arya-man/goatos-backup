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
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	secondTenant         = "00000000-0000-4000-8000-000000000002"
	meshaParty           = "00000000-0000-4000-8000-000000001001"
	cbeLocation          = "00000000-0000-4000-8000-000000003001"
	cptLocation          = "00000000-0000-4000-8000-000000003002"
	t2Location           = "00000000-0000-4000-8000-000000003101"
)

func TestRepositoryReadPathsWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

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
	if passport.DisplayID == "" || passport.Summary.AnimalIdentifier1 == nil || *passport.Summary.AnimalIdentifier1 != "A1-1900-CBE" {
		t.Fatalf("unexpected passport: %#v", passport)
	}
	if len(passport.Identifiers) != 2 || passport.Identifiers[0].IdentifierValue != "A1-1900-CBE" {
		t.Fatalf("unexpected passport identifiers: %#v", passport.Identifiers)
	}

	displayPassport, err := repo.GetGoatByDisplayID(ctx, meshaTenant, passport.DisplayID)
	if err != nil {
		t.Fatalf("GetGoatByDisplayID: %v", err)
	}
	if displayPassport.GoatID != passport.GoatID || displayPassport.DisplayID != passport.DisplayID {
		t.Fatalf("display lookup returned wrong goat: %#v", displayPassport)
	}
	if len(displayPassport.Identifiers) != 2 || displayPassport.Identifiers[0].ScopeKey != "global" {
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
		IdentifierType:  "animal_identifier_1",
		NormalizedValue: "A1-1900-CBE",
		ScopeKey:        strPtr("global"),
	})
	if err != nil {
		t.Fatalf("FindIdentifierMatches scoped: %v", err)
	}
	if len(matches) != 1 || matches[0].Goat.GoatID != "10000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected scoped matches: %#v", matches)
	}

	matches, err = repo.FindIdentifierMatches(ctx, ports.ResolveIdentifierParams{
		TenantID:        meshaTenant,
		IdentifierType:  "animal_identifier_1",
		NormalizedValue: "A1-1900-CBE",
	})
	if err != nil {
		t.Fatalf("FindIdentifierMatches all scopes: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("Animal ID is lifetime-unique and should return 1 visible match, got %d", len(matches))
	}

	// Regression (mobile shifting crash, 2026-08-19): a goat with TWO active rows of the SAME
	// identifier type (secondary RFID + legacy tag — 172 real STG animals) must still be ONE search
	// row. The old per-type LEFT JOINs fanned the goat out once per identifier, and the shifting
	// picker crashed on the duplicate list key. Grain proof: goatSummarySelect's identifier join is
	// a LATERAL aggregate over (tenant_id, goat_id), so it is 1:1 with goats by construction, and
	// both identifier values survive into the one row's display instead of one being dropped.
	dupSearch, _, err := repo.SearchGoats(ctx, ports.SearchGoatsParams{
		TenantID: meshaTenant,
		Query:    strPtr("901007000505001"),
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("SearchGoats dup-identifier goat: %v", err)
	}
	if len(dupSearch) != 1 {
		t.Fatalf("a goat with two active animal_identifier_2 rows must be ONE search row, got %d: %#v", len(dupSearch), dupSearch)
	}
	if dupSearch[0].GoatID != "10000000-0000-4000-8000-000000000003" {
		t.Fatalf("unexpected dup-identifier match: %#v", dupSearch[0])
	}
	if dupSearch[0].AnimalIdentifier2 == nil ||
		!strings.Contains(*dupSearch[0].AnimalIdentifier2, "901007000505001") ||
		!strings.Contains(*dupSearch[0].AnimalIdentifier2, "CBE-9001") {
		t.Fatalf("both active secondary identifiers must survive into the one row's display, got %v", dupSearch[0].AnimalIdentifier2)
	}
	// The legacy tag must find the same single row too.
	dupSearch, _, err = repo.SearchGoats(ctx, ports.SearchGoatsParams{
		TenantID: meshaTenant,
		Query:    strPtr("CBE-9001"),
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("SearchGoats dup-identifier goat by legacy tag: %v", err)
	}
	if len(dupSearch) != 1 || dupSearch[0].GoatID != "10000000-0000-4000-8000-000000000003" {
		t.Fatalf("legacy-tag search must return the same single row, got %#v", dupSearch)
	}

	conflict, err := repo.FindOpenConflictForIdentifier(ctx, meshaTenant, "animal_identifier_1", "A1-1900-CBE", "global")
	if err != nil {
		t.Fatalf("FindOpenConflictForIdentifier: %v", err)
	}
	if conflict == nil || *conflict != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("unexpected conflict id: %v", conflict)
	}
	conflict, err = repo.FindOpenConflictForIdentifier(ctx, meshaTenant, "animal_identifier_1", "A1-NOT-RECORDED", "global")
	if err != nil {
		t.Fatalf("FindOpenConflictForIdentifier absent id: %v", err)
	}
	if conflict != nil {
		t.Fatalf("unexpected conflict id for absent Animal ID: %v", *conflict)
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

INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, breed, sex)
VALUES
  ('10000000-0000-4000-8000-000000000001', '`+meshaTenant+`', 'alive', 'goat', '`+meshaParty+`', '`+cbeLocation+`', '`+cbeLocation+`', 'Synthetic Boer', 'female'),
  ('10000000-0000-4000-8000-000000000002', '`+meshaTenant+`', 'alive', 'goat', '`+meshaParty+`', '`+cptLocation+`', '`+cptLocation+`', 'Synthetic Boer', 'male'),
  ('10000000-0000-4000-8000-000000000003', '`+meshaTenant+`', 'alive', 'goat', '`+meshaParty+`', '`+cbeLocation+`', '`+cbeLocation+`', 'Synthetic Boer', 'female'),
  ('10000000-0000-4000-8000-000000000101', '`+secondTenant+`', 'alive', 'goat', '`+meshaParty+`', '`+t2Location+`', '`+t2Location+`', 'Synthetic Boer', 'female');

	INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
	VALUES
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000001', 'animal_identifier_1', 'A1-1900-CBE', 'A1-1900-CBE', 'global', true, 'active', now(), 'test_v1'),
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000001', 'animal_identifier_2', 'A2-1900-CBE', 'A2-1900-CBE', 'global', false, 'active', now(), 'test_v1'),
	  -- The dup-tag goat: TWO active animal_identifier_2 rows (a secondary RFID plus a legacy farm
	  -- tag), the real STG shape that made every goat_identifiers display join fan out one search row
	  -- per identifier and crash the mobile shifting picker on duplicate list keys.
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000003', 'animal_identifier_1', '901007000503001', '901007000503001', 'global', true, 'active', now(), 'test_v1'),
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000003', 'animal_identifier_2', '901007000505001', '901007000505001', 'global', false, 'active', now(), 'test_v1'),
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000003', 'animal_identifier_2', 'CBE-9001', 'CBE-9001', 'global', false, 'active', now(), 'test_v1'),
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000002', 'animal_identifier_1', 'A1-1900-CPT', 'A1-1900-CPT', 'global', true, 'active', now(), 'test_v1'),
	  ('`+meshaTenant+`', '10000000-0000-4000-8000-000000000002', 'animal_identifier_2', 'A2-1900-CPT', 'A2-1900-CPT', 'global', false, 'active', now(), 'test_v1'),
	  ('`+secondTenant+`', '10000000-0000-4000-8000-000000000101', 'animal_identifier_1', 'A1-1900-T2', 'A1-1900-T2', 'global', true, 'active', now(), 'test_v1'),
	  ('`+secondTenant+`', '10000000-0000-4000-8000-000000000101', 'animal_identifier_2', 'A2-1900-T2', 'A2-1900-T2', 'global', false, 'active', now(), 'test_v1');

INSERT INTO identity_conflicts (conflict_id, tenant_id, conflict_type, severity, state, identifier_type, identifier_value, goat_ids, source_record_ids, evidence)
VALUES (
  '20000000-0000-4000-8000-000000000001',
  '`+meshaTenant+`',
	  'status_mismatch',
  'medium',
  'open',
	  'animal_identifier_1',
	  'A1-1900-CBE',
  ARRAY['10000000-0000-4000-8000-000000000001'::uuid, '10000000-0000-4000-8000-000000000002'::uuid],
  ARRAY['synthetic-source-record-1'],
  '{
	    "scope_key":"global",
	    "source_system":"goatos_seed",
	    "source_context":"clean_slate_fixture",
	    "review_note":"Synthetic clean-slate evidence for repository UI.",
    "latest_event_date":"2026-06-01",
    "latest_farm_code":"CBE",
	    "matched_keys":["animal_identifier_1:global:A1-1900-CBE"],
    "conflicts":[{
      "Attribute":"sex",
	      "Reason":"source_fixture_conflict",
      "LocalValue":"female",
      "ConflictingValue":"female|male",
	      "IdentifierKind":"animal_identifier_1",
	      "IdentifierKey":"animal_identifier_1:global:A1-1900-CBE",
      "LatestEventDate":"2026-06-01",
	      "RecommendedState":"Clean-slate fixture conflict for repository coverage."
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
	  'animal_identifier_1',
	  'A1-1900-CPT',
	  ARRAY['10000000-0000-4000-8000-000000000002'::uuid],
	  ARRAY['animal_identifier_1:global:A1-1900-CPT'],
	  '{"scope_key":"global","source_system":"goatos_seed"}'::jsonb
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
