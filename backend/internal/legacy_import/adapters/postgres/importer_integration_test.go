package postgres

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	secondTenant         = "00000000-0000-4000-8000-000000000002"
	longRFID             = "9900000000000000001234567890123"
)

func TestRFIDWorkbookImportRunnerWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startLegacyImportDB(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	importer := legacy_import.NewImporter(repo)
	fixture := filepath.Join(repoRoot(t), "backend", "internal", "legacy_import", "testdata", "synthetic_rfid_import.xlsx")

	t.Run("dry-run writes only import run with policy aggregate counts", func(t *testing.T) {
		result, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     fixture,
			TenantID:      meshaTenant,
			DryRun:        true,
			BatchSize:     3,
			SourceName:    "Synthetic RFID dry run",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("dry-run import: %v", err)
		}
		if !result.DryRun || result.SourceSystem != "legacy_rfid_db" || result.SourceDataset != "rfid_db_first_import" || result.PolicyVersion != legacy_import.DefaultPolicyVersion {
			t.Fatalf("policy/source mismatch: %#v", result)
		}
		if result.RowCount != 12 || result.ErrorCount != 2 || result.RowsInserted != 0 {
			t.Fatalf("unexpected dry-run counts: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_runs WHERE import_run_id = $1 AND dry_run AND status = 'completed' AND row_count = 12 AND error_count = 2 AND source_system = 'legacy_rfid_db' AND source_dataset = 'rfid_db_first_import' AND source_file_ref IS NULL AND source_file_hash LIKE 'sha256:%'`, result.ImportRunID); got != 1 {
			t.Fatalf("dry-run row count = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE import_run_id = $1`, result.ImportRunID); got != 0 {
			t.Fatalf("dry-run staged rows = %d", got)
		}
		assertNoCanonicalIdentityMutation(t, pool)
	})

	t.Run("real staging persists normalized rows and uses five-column conflict key", func(t *testing.T) {
		result, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     fixture,
			TenantID:      meshaTenant,
			BatchSize:     4,
			SourceName:    "Synthetic RFID staging run",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("real import: %v", err)
		}
		if result.DryRun || result.RowCount != 12 || result.ErrorCount != 2 || result.RowsInserted != 12 {
			t.Fatalf("unexpected real import result: %#v", result)
		}
		assertRunCompleted(t, pool, result.ImportRunID, false, 12, 2)
		assertStateCount(t, pool, result.ImportRunID, legacy_import.StatePending, 5)
		assertStateCount(t, pool, result.ImportRunID, legacy_import.StateNeedsReview, 5)
		assertStateCount(t, pool, result.ImportRunID, legacy_import.StateError, 2)
		assertRowState(t, pool, result.ImportRunID, 3, legacy_import.StateError, "duplicate_rfid_in_workbook")
		assertRowState(t, pool, result.ImportRunID, 5, legacy_import.StateNeedsReview, "duplicate_old_tag_same_scope")
		assertRowState(t, pool, result.ImportRunID, 7, legacy_import.StatePending, "")
		assertRowState(t, pool, result.ImportRunID, 8, legacy_import.StatePending, "")
		assertRowState(t, pool, result.ImportRunID, 9, legacy_import.StateNeedsReview, "blank_old_tag_suffix")
		assertRowState(t, pool, result.ImportRunID, 10, legacy_import.StateNeedsReview, "blank_gender")
		assertRowState(t, pool, result.ImportRunID, 13, legacy_import.StateNeedsReview, "missing_rfid")

		var rawRFID, normalizedRFID, key, keyRecipe, hashRecipe string
		if err := pool.QueryRow(ctx, `
SELECT raw_payload->>'RFID',
       normalized_payload->>'rfid',
       source_row_key,
       source_key_recipe_version,
       hash_recipe_version
FROM legacy_import_rows
WHERE tenant_id = $1 AND import_run_id = $2 AND row_number = 2`, meshaTenant, result.ImportRunID).Scan(&rawRFID, &normalizedRFID, &key, &keyRecipe, &hashRecipe); err != nil {
			t.Fatal(err)
		}
		if rawRFID != longRFID || normalizedRFID != longRFID {
			t.Fatalf("RFID precision lost raw=%q normalized=%q", rawRFID, normalizedRFID)
		}
		if strings.HasPrefix(key, meshaTenant) || !strings.Contains(key, "source_system=legacy_rfid_db") {
			t.Fatalf("bad source row key: %s", key)
		}
		if keyRecipe != "source_key_recipe_v1" || hashRecipe != "hash_recipe_v1" {
			t.Fatalf("recipe versions = %s/%s", keyRecipe, hashRecipe)
		}
		var sex string
		var growth string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(normalized_payload->>'sex', ''), normalized_payload->>'growth_cohort_tag' FROM legacy_import_rows WHERE import_run_id = $1 AND row_number = 10`, result.ImportRunID).Scan(&sex, &growth); err != nil {
			t.Fatal(err)
		}
		if sex != "" || growth != "F2" {
			t.Fatalf("F2 blank-gender row inferred sex: sex=%s growth=%s", sex, growth)
		}

		again, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     fixture,
			TenantID:      meshaTenant,
			BatchSize:     5,
			SourceName:    "Synthetic RFID repeated staging run",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("repeat import: %v", err)
		}
		if again.RowsInserted != 0 {
			t.Fatalf("repeat import inserted duplicate rows: %#v", again)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE tenant_id = $1`, meshaTenant); got != 12 {
			t.Fatalf("rows after repeat import = %d", got)
		}

		if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Synthetic tenant for import', 'active') ON CONFLICT DO NOTHING`, secondTenant); err != nil {
			t.Fatal(err)
		}
		tenantTwo, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     fixture,
			TenantID:      secondTenant,
			BatchSize:     6,
			SourceName:    "Synthetic tenant two RFID staging run",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("second tenant import: %v", err)
		}
		if tenantTwo.RowsInserted != 12 {
			t.Fatalf("five-column uniqueness did not allow second tenant rows: %#v", tenantTwo)
		}
	})

	t.Run("changed normalized projection keeps key and routes to review", func(t *testing.T) {
		modified := filepath.Join(t.TempDir(), "synthetic_changed.xlsx")
		writeSyntheticWorkbook(t, modified, [][]string{
			{"Farm", "Old ID", "Old ID Suffix", "RFID", "Age", "Gender", "Breed", "Tag", "Shed", "Partition"},
			{"CBE", "1900", "CBE", longRFID, "Adult", "Female", "Saanen", "", "S1", "P1"},
		})
		result, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     modified,
			TenantID:      meshaTenant,
			BatchSize:     2,
			SourceName:    "Synthetic changed RFID staging run",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("changed import: %v", err)
		}
		if result.RowsInserted != 1 {
			t.Fatalf("changed import rows inserted = %d", result.RowsInserted)
		}
		assertRowState(t, pool, result.ImportRunID, 2, legacy_import.StateNeedsReview, "source_row_changed")
		if got := countRows(t, pool, `
SELECT count(DISTINCT source_row_version_hash)
FROM legacy_import_rows
WHERE tenant_id = $1
  AND source_row_key = (
    SELECT source_row_key FROM legacy_import_rows WHERE import_run_id = $2 AND row_number = 2
  )`, meshaTenant, result.ImportRunID); got != 2 {
			t.Fatalf("changed row did not create second hash version, distinct hashes=%d", got)
		}
	})

	t.Run("stale duplicate old-tag review requeues when current source is clean", func(t *testing.T) {
		dir := t.TempDir()
		oldSnapshot := filepath.Join(dir, "stale_duplicate_old.xlsx")
		writeSyntheticWorkbook(t, oldSnapshot, [][]string{
			{"Farm", "Old ID", "Old ID Suffix", "RFID", "Age", "Gender", "Breed", "Tag", "Shed", "Partition"},
			{"CPT", "878", "BLR", "9900000000000000000000009500001", "Adult", "Female", "Boer", "", "Castro 2", ""},
			{"CPT", "878", "BLR", "9900000000000000000000009500002", "Adult", "Female", "Boer", "", "Castro 2", ""},
		})
		oldRun, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     oldSnapshot,
			TenantID:      meshaTenant,
			BatchSize:     2,
			SourceName:    "Synthetic stale duplicate old snapshot",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("old duplicate import: %v", err)
		}
		if oldRun.RowsInserted != 2 {
			t.Fatalf("old duplicate rows inserted=%d, want 2", oldRun.RowsInserted)
		}
		assertRowState(t, pool, oldRun.ImportRunID, 2, legacy_import.StateNeedsReview, "duplicate_old_tag_same_scope")
		assertRowState(t, pool, oldRun.ImportRunID, 3, legacy_import.StateNeedsReview, "duplicate_old_tag_same_scope")

		current := filepath.Join(dir, "stale_duplicate_current.xlsx")
		writeSyntheticWorkbook(t, current, [][]string{
			{"Farm", "Old ID", "Old ID Suffix", "RFID", "Age", "Gender", "Breed", "Tag", "Shed", "Partition"},
			{"CPT", "878", "BLR", "9900000000000000000000009500001", "Adult", "Female", "Boer", "", "Castro 2", ""},
			{"CPT", "876", "BLR", "9900000000000000000000009500002", "Adult", "Female", "Boer", "", "Castro 2", ""},
		})
		currentRun, err := importer.ImportRFIDWorkbook(ctx, legacy_import.ImportCommand{
			InputPath:     current,
			TenantID:      meshaTenant,
			BatchSize:     2,
			SourceName:    "Synthetic stale duplicate current source",
			PolicyVersion: legacy_import.DefaultPolicyVersion,
		})
		if err != nil {
			t.Fatalf("current import: %v", err)
		}
		assertRowState(t, pool, currentRun.ImportRunID, 2, legacy_import.StatePending, "")
		assertRowState(t, pool, currentRun.ImportRunID, 3, legacy_import.StatePending, "")
		assertStateCount(t, pool, currentRun.ImportRunID, legacy_import.StatePending, 2)
		if got := countRows(t, pool, `
SELECT count(*)
FROM legacy_import_rows
WHERE tenant_id = $1
  AND import_run_id = $2
  AND normalized_payload->>'normalized_old_tag' = '878'
  AND normalized_payload @> '{"processing_reasons":["duplicate_old_tag_same_scope"]}'::jsonb`, meshaTenant, currentRun.ImportRunID); got != 0 {
			t.Fatalf("current run retained stale duplicate review rows=%d, want 0", got)
		}
	})
}

func startLegacyImportDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container := fmt.Sprintf("goatos-legacy-import-test-%d", time.Now().UnixNano())
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
	return openPool(t, ctx, container)
}

func assertRunCompleted(t *testing.T, pool *pgxpool.Pool, runID string, dryRun bool, rows, errors int) {
	t.Helper()
	got := countRows(t, pool, `
SELECT count(*)
FROM legacy_import_runs
WHERE import_run_id = $1
  AND dry_run = $2
  AND status = 'completed'
  AND row_count = $3
  AND error_count = $4
  AND created_goat_count = 0
  AND updated_goat_count = 0
  AND conflict_count = 0
  AND source_system = 'legacy_rfid_db'
  AND source_dataset = 'rfid_db_first_import'
  AND policy_version = 'phase1-rfid-db-import-v1'
  AND source_file_ref IS NULL
  AND source_file_hash LIKE 'sha256:%'`, runID, dryRun, rows, errors)
	if got != 1 {
		t.Fatalf("completed run rows = %d", got)
	}
}

func assertStateCount(t *testing.T, pool *pgxpool.Pool, runID, state string, want int) {
	t.Helper()
	if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE import_run_id = $1 AND processing_state = $2`, runID, state); got != want {
		t.Fatalf("state %s count=%d, want %d", state, got, want)
	}
}

func assertRowState(t *testing.T, pool *pgxpool.Pool, runID string, rowNumber int, state, reasonFragment string) {
	t.Helper()
	var gotState string
	var reason *string
	var payload []byte
	if err := pool.QueryRow(context.Background(), `
SELECT processing_state, error_reason, normalized_payload
FROM legacy_import_rows
WHERE import_run_id = $1 AND row_number = $2`, runID, rowNumber).Scan(&gotState, &reason, &payload); err != nil {
		t.Fatal(err)
	}
	if gotState != state {
		t.Fatalf("row %d state=%s, want %s", rowNumber, gotState, state)
	}
	if reasonFragment == "" {
		return
	}
	if reason != nil && strings.Contains(*reason, reasonFragment) {
		return
	}
	var normalized map[string]any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		t.Fatal(err)
	}
	for _, item := range normalized["processing_reasons"].([]any) {
		if item == reasonFragment {
			return
		}
	}
	t.Fatalf("row %d reason=%v payload=%s, want fragment %s", rowNumber, reason, string(payload), reasonFragment)
}

func assertNoCanonicalIdentityMutation(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for label, query := range map[string]string{
		"goats":                  `SELECT count(*) FROM goats`,
		"goat_identifiers":       `SELECT count(*) FROM goat_identifiers`,
		"goat_identity_events":   `SELECT count(*) FROM goat_identity_events`,
		"outbox_messages":        `SELECT count(*) FROM outbox_messages`,
		"goat_identity_counters": `SELECT count(*) FROM goat_identity_counters`,
	} {
		if got := countRows(t, pool, query); got != 0 {
			t.Fatalf("%s rows = %d", label, got)
		}
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
		t.Fatalf("%s failed: %v\n%s", name, err, stderr.String())
	}
	return string(out)
}

func writeSyntheticWorkbook(t *testing.T, path string, rows [][]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(t, zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`)
	writeZipFile(t, zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`)
	writeZipFile(t, zw, "xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Combined" sheetId="1" r:id="rId1"/></sheets>
</workbook>`)
	writeZipFile(t, zw, "xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`)
	writeZipFile(t, zw, "xl/worksheets/sheet1.xml", worksheetXML(rows))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func worksheetXML(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIdx, row := range rows {
		b.WriteString(fmt.Sprintf(`<row r="%d">`, rowIdx+1))
		for colIdx, value := range row {
			ref := columnName(colIdx+1) + fmt.Sprint(rowIdx+1)
			b.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEscape(value)))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func columnName(idx int) string {
	var out []byte
	for idx > 0 {
		idx--
		out = append([]byte{byte('A' + idx%26)}, out...)
		idx /= 26
	}
	return string(out)
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
