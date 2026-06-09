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
	if _, err := repo.GetConflict(ctx, secondTenant, "20000000-0000-4000-8000-000000000001"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-tenant conflict lookup should return ErrNotFound, got %v", err)
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
  '{"scope_key":"park:CBE"}'::jsonb
);

INSERT INTO identity_conflict_goats (conflict_id, tenant_id, goat_id, role)
VALUES
  ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', '10000000-0000-4000-8000-000000000001', 'affected'),
  ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', '10000000-0000-4000-8000-000000000002', 'affected');

INSERT INTO identity_conflict_source_records (conflict_id, tenant_id, source_system, source_record_id)
VALUES ('20000000-0000-4000-8000-000000000001', '`+meshaTenant+`', 'synthetic_import', 'synthetic-source-record-1');

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
