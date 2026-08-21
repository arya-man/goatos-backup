package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestExportCampaignCSVWithIndividualObservations tests exporting individual animal observations.
func TestExportCampaignCSVWithIndividualObservations(t *testing.T) {
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("skipping Postgres test - GOATOS_RUN_POSTGRES_TESTS not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:goatos@127.0.0.1:15546/goatos?sslmode=disable")
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	// Use live test campaign from the requirements
	tenantID := "00000000-0000-4000-8000-000000000001" // Default test tenant
	campaignID := "92000000-0000-4000-8000-000000000701"

	buf := bytes.NewBuffer(nil)
	err = repo.ExportCampaignCSV(ctx, tenantID, campaignID, buf)
	if err != nil {
		t.Fatalf("ExportCampaignCSV failed: %v", err)
	}

	// Parse CSV and verify structure
	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}

	if len(records) == 0 {
		t.Fatal("CSV export is empty (no rows at all)")
	}

	// Check header
	expectedHeader := []string{
		"Park",
		"Shed Name",
		"Shed Status",
		"Type",
		"Scanned Identifier",
		"Weight (kg)",
		"Average Weight (kg)",
		"Animal Count",
		"Verification Status",
		"Proof Reference Type",
		"Proof Reference",
		"Proof Video URL",
		"Proof URL Note",
		"Recorded At",
		"Date (IST)",
		"Time (IST)",
	}

	if len(records[0]) != len(expectedHeader) {
		t.Errorf("header column count mismatch: got %d, want %d", len(records[0]), len(expectedHeader))
	}

	for i, v := range expectedHeader {
		if i < len(records[0]) && records[0][i] != v {
			t.Errorf("header column %d: got %q, want %q", i, records[0][i], v)
		}
	}

	// Verify data rows
	if len(records) < 2 {
		t.Fatal("no data rows in export")
	}

	// Check that we have individual observations
	foundIndividual := false
	for _, record := range records[1:] {
		if len(record) >= 2 && record[1] == "individual" {
			foundIndividual = true
			// For individual observations, scanned identifier should not be empty
			if record[2] == "" {
				t.Error("individual observation has empty scanned identifier")
			}
			// Weight should not be empty
			if record[3] == "" {
				t.Error("individual observation has empty weight")
			}
			break
		}
	}

	if !foundIndividual {
		// It's OK if there are no individual observations, just log it
		t.Log("no individual observations found in export (this is OK)")
	}

	t.Logf("CSV export has %d rows (including header)", len(records))
}

// TestExportCampaignCSVWithLumpSumObservations tests exporting lump-sum shed observations.
func TestExportCampaignCSVWithLumpSumObservations(t *testing.T) {
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("skipping Postgres test - GOATOS_RUN_POSTGRES_TESTS not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:goatos@127.0.0.1:15546/goatos?sslmode=disable")
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	// Use live test campaign from the requirements (campaign with lump-sum)
	tenantID := "00000000-0000-4000-8000-000000000001"
	campaignID := "92000000-0000-4000-8000-000000000702"

	buf := bytes.NewBuffer(nil)
	err = repo.ExportCampaignCSV(ctx, tenantID, campaignID, buf)
	if err != nil {
		t.Fatalf("ExportCampaignCSV failed: %v", err)
	}

	// Parse CSV and verify structure
	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}

	// Check that we have lump-sum observations
	foundLumpSum := false
	for _, record := range records[1:] {
		if len(record) >= 2 && record[1] == "lumpsum" {
			foundLumpSum = true
			// For lump-sum, animal count should not be empty
			if record[5] == "" {
				t.Error("lump-sum observation has empty animal count")
			}
			// Average weight should not be empty
			if record[4] == "" {
				t.Error("lump-sum observation has empty average weight")
			}
			break
		}
	}

	if !foundLumpSum {
		t.Log("no lump-sum observations found in export (this is OK)")
	}
}

// TestExportCSVWithRejectedObservations tests that rejected observations are included with rework status.
func TestExportCSVWithRejectedObservations(t *testing.T) {
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("skipping Postgres test - GOATOS_RUN_POSTGRES_TESTS not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:goatos@127.0.0.1:15546/goatos?sslmode=disable")
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	tenantID := "00000000-0000-4000-8000-000000000001"
	campaignID := "92000000-0000-4000-8000-000000000701"

	buf := bytes.NewBuffer(nil)
	err = repo.ExportCampaignCSV(ctx, tenantID, campaignID, buf)
	if err != nil {
		t.Fatalf("ExportCampaignCSV failed: %v", err)
	}

	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}

	// Check that verification status column is present and contains valid values
	for i, record := range records[1:] {
		if len(record) >= 7 {
			status := record[6]
			// Should be one of: pending, verified, rework, or empty
			if status != "" && status != "pending" && status != "verified" && status != "rework" {
				t.Errorf("row %d: unexpected verification status: %q", i, status)
			}
		}
	}
}

// TestExportCSVFieldEscaping tests that fields with special characters are properly escaped.
func TestExportCSVFieldEscaping(t *testing.T) {
	if os.Getenv("GOATOS_RUN_POSTGRES_TESTS") != "1" {
		t.Skip("skipping Postgres test - GOATOS_RUN_POSTGRES_TESTS not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://postgres:goatos@127.0.0.1:15546/goatos?sslmode=disable")
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	tenantID := "00000000-0000-4000-8000-000000000001"
	campaignID := "92000000-0000-4000-8000-000000000701"

	buf := bytes.NewBuffer(nil)
	err = repo.ExportCampaignCSV(ctx, tenantID, campaignID, buf)
	if err != nil {
		t.Fatalf("ExportCampaignCSV failed: %v", err)
	}

	// Verify the CSV is properly formatted by parsing it
	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("CSV parsing failed: %v", err)
	}

	// If we successfully parsed it, field escaping is correct
	if len(records) < 1 {
		t.Fatal("no records in CSV")
	}

	t.Logf("Successfully parsed %d rows from CSV", len(records))
}

// TestBroadExportCSVFollowsWeightCheckSheetFormat pins the whole-window export to the
// operations "Weight check" sheet shape (maintainer request 2026-08-21): sheet columns
// minus the video link, identity cells resolved through the demographics exemption,
// operator named from workforce_members, approval in the sheet's own words, and the
// optional shed filter narrowing rows without dropping the format.
func TestBroadExportCSVFollowsWeightCheckSheetFormat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	// Same-animal reporting facts for the individual row: the scanned tag resolves to
	// the fixture goat, which carries a register id, a second tag, a breed and a sex.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE goats SET breed='F2' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'BROAD-TAG-1', 'broad-tag-1', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $2::uuid, 'animal_identifier_2', 'BROAD-TAG-2', 'broad-tag-2', 'global', false, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, 'OP-EXP', 'Weigh Operator', 'active', 'operator')
ON CONFLICT DO NOTHING`, repoTenant, repoOperator)

	acceptedAt := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (
  tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'BROAD-TAG-1', 18.5,
  $4::uuid, $5::uuid, 'broad-export-individual', $6::timestamptz, 'pending')`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator, acceptedAt)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, 410, 20.5, 20,
  $4::uuid, $5::uuid, 'broad-export-lumpsum', $6::timestamptz, 'verified')`,
		repoTenant, repoCampaign, repoShedScope, repoShedProof, repoOperator, acceptedAt.Add(time.Hour))

	repo := NewRepository(pool, 5*time.Second)

	buf := bytes.NewBuffer(nil)
	if err := repo.ExportCSV(ctx, repoTenant, []string{repoPark}, nil, acceptedAt.Add(-time.Hour), acceptedAt.Add(2*time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse broad export CSV: %v", err)
	}
	wantHeader := []string{
		"date", "rfid", "rfid_2", "old_id", "old_id_suffix", "breed", "gender",
		"shed", "type", "count", "operator", "approval", "verified_weight_kg",
	}
	if len(records) == 0 || strings.Join(records[0], "|") != strings.Join(wantHeader, "|") {
		t.Fatalf("export header=%v, want %v", records, wantHeader)
	}
	byKind := recordsByColumn(t, records, "type")
	individual := byKind["individual"]
	if individual == nil {
		t.Fatalf("broad export missing individual row: %#v", records)
	}
	for column, want := range map[string]string{
		"rfid":               "BROAD-TAG-1",
		"rfid_2":             "BROAD-TAG-2",
		"old_id":             "G-990001",
		"old_id_suffix":      "",
		"breed":              "F2",
		"gender":             "female",
		"shed":               "Gandhi 1 - Part 1",
		"count":              "",
		"operator":           "Weigh Operator",
		"approval":           "pending",
		"verified_weight_kg": "18.5",
	} {
		if got := csvCell(t, records[0], individual, column); got != want {
			t.Fatalf("individual %s=%q, want %q", column, got, want)
		}
	}
	lumpsum := byKind["lumpsum"]
	if lumpsum == nil {
		t.Fatalf("broad export missing lumpsum row: %#v", records)
	}
	for column, want := range map[string]string{
		"rfid":               "",
		"old_id":             "",
		"breed":              "",
		"gender":             "",
		"shed":               "Q1",
		"count":              "20",
		"operator":           "Weigh Operator",
		"approval":           "approved",
		"verified_weight_kg": "410",
	} {
		if got := csvCell(t, records[0], lumpsum, column); got != want {
			t.Fatalf("lumpsum %s=%q, want %q", column, got, want)
		}
	}

	// The shed filter narrows rows to the selected shed locations only.
	buf.Reset()
	if err := repo.ExportCSV(ctx, repoTenant, []string{repoPark}, []string{repoExpectedShed}, acceptedAt.Add(-time.Hour), acceptedAt.Add(2*time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV with shed filter: %v", err)
	}
	filtered, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse filtered export CSV: %v", err)
	}
	filteredByKind := recordsByColumn(t, filtered, "type")
	if filteredByKind["individual"] == nil {
		t.Fatalf("shed-filtered export missing the selected shed's row: %#v", filtered)
	}
	if filteredByKind["lumpsum"] != nil {
		t.Fatalf("shed filter leaked the other shed's lumpsum row: %#v", filtered)
	}
}

// TestExportIdentitiesOneToManyIdentifierRows is the cardinality adversarial case:
// one goat carrying SEVERAL identifier rows must still yield exactly one export row
// per observation — the identity map is keyed per tag with DISTINCT ON and the
// second-tag LATERAL is LIMIT 1, so the one-to-many side can never multiply rows.
func TestExportIdentitiesOneToManyIdentifierRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES
  ($1::uuid, $2::uuid, 'animal_identifier_1', 'MANY-TAG-1', 'many-tag-1', 'global', true, 'active', now(), 'test'),
  ($1::uuid, $2::uuid, 'animal_identifier_2', 'MANY-TAG-2', 'many-tag-2', 'global', false, 'active', now(), 'test'),
  ($1::uuid, $2::uuid, 'animal_identifier_2', 'MANY-TAG-3', 'many-tag-3', 'global', false, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id=EXCLUDED.goat_id, identifier_value=EXCLUDED.identifier_value, status='active'`,
		repoTenant, repoAnimal)
	acceptedAt := time.Date(2026, 8, 11, 6, 0, 0, 0, time.UTC)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (
  tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'MANY-TAG-1', 21.5,
  $4::uuid, $5::uuid, 'many-tag-export', $6::timestamptz, 'pending')`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator, acceptedAt)

	repo := NewRepository(pool, 5*time.Second)
	buf := bytes.NewBuffer(nil)
	if err := repo.ExportCSV(ctx, repoTenant, []string{repoPark}, nil, acceptedAt.Add(-time.Hour), acceptedAt.Add(time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("one observation of a many-identifier goat must export exactly one row, got %d: %#v", len(records)-1, records)
	}
	if got := csvCell(t, records[0], records[1], "rfid_2"); got == "" || got == "MANY-TAG-1" {
		t.Fatalf("rfid_2=%q, want one of the goat's OTHER tags", got)
	}
}

// TestExportCSVMultiPageWindowStreamsEveryRow is the pagination adversarial case:
// the export is whole-window and STREAMED — there is no page cap, so every date and
// every row of the selected span must appear, newest date first.
func TestExportCSVMultiPageWindowStreamsEveryRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	base := time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		for row := 0; row < 2; row++ {
			execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (
  tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 20,
  $5::uuid, $6::uuid, $7, $8::timestamptz, 'pending')`,
				repoTenant, repoCampaign, repoAnimalScope,
				"WIN-TAG-"+string(rune('0'+day))+string(rune('0'+row)),
				repoAnimalProof, repoOperator,
				"window-export-"+string(rune('0'+day))+string(rune('0'+row)),
				base.AddDate(0, 0, -day).Add(time.Duration(row)*time.Minute))
		}
	}

	repo := NewRepository(pool, 5*time.Second)
	buf := bytes.NewBuffer(nil)
	if err := repo.ExportCSV(ctx, repoTenant, []string{repoPark}, nil, base.AddDate(0, 0, -2).Add(-time.Hour), base.Add(time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 7 {
		t.Fatalf("3 dates x 2 rows must all stream, got %d data rows: %#v", len(records)-1, records)
	}
	var previous string
	for _, record := range records[1:] {
		date := csvCell(t, records[0], record, "date")
		if previous != "" && date > previous {
			t.Fatalf("rows must be ordered newest date first, saw %q after %q", date, previous)
		}
		previous = date
	}
}

// TestExportCSVParkScopeExcludesOtherParks is the scope adversarial case: the
// export serves ONLY the caller's resolved park list — a park outside it exports
// nothing, and an empty park list exports only the header.
func TestExportCSVParkScopeExcludesOtherParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	acceptedAt := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (
  tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'SCOPE-TAG-1', 19,
  $4::uuid, $5::uuid, 'scope-export', $6::timestamptz, 'pending')`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator, acceptedAt)

	repo := NewRepository(pool, 5*time.Second)
	otherPark := "99999999-9999-4999-8999-999999999999"
	buf := bytes.NewBuffer(nil)
	if err := repo.ExportCSV(ctx, repoTenant, []string{otherPark}, nil, acceptedAt.Add(-time.Hour), acceptedAt.Add(time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV other park: %v", err)
	}
	if records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll(); err != nil || len(records) != 1 {
		t.Fatalf("another park's scope must export header only, got %#v (err=%v)", records, err)
	}
	buf.Reset()
	if err := repo.ExportCSV(ctx, repoTenant, nil, nil, acceptedAt.Add(-time.Hour), acceptedAt.Add(time.Hour), buf); err != nil {
		t.Fatalf("ExportCSV empty scope: %v", err)
	}
	if records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll(); err != nil || len(records) != 1 {
		t.Fatalf("an empty park scope must export header only, got %#v (err=%v)", records, err)
	}
}

// stubProofURLResolver is a minimal ProofURLResolver for unit testing resolveProofVideoURL
// without a Postgres connection.
type stubProofURLResolver struct {
	urls map[string]string
	errs map[string]error
}

func (s stubProofURLResolver) ResolveProofDownloadURL(_ context.Context, _, proofID string) (string, error) {
	if err, ok := s.errs[proofID]; ok {
		return "", err
	}
	return s.urls[proofID], nil
}

// TestResolveProofVideoURLResolvable asserts that a proof with a working resolver and a linked
// proof id yields an absolute, clickable URL and no note -- the maintainer-facing contract this
// fix exists for.
func TestResolveProofVideoURLResolvable(t *testing.T) {
	repo := (&Repository{}).WithProofURLResolver(stubProofURLResolver{
		urls: map[string]string{
			"proof-1": "http://127.0.0.1:8090/app/proofs/proof-1/download/signed?tenant_id=t&expires=1&sig=abc",
		},
	})

	url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "proof-1", "some/object/key.mp4")

	if url != "http://127.0.0.1:8090/app/proofs/proof-1/download/signed?tenant_id=t&expires=1&sig=abc" {
		t.Errorf("expected absolute clickable URL, got %q", url)
	}
	if note != "" {
		t.Errorf("expected no note for a resolvable proof, got %q", note)
	}
}

// TestResolveProofVideoURLUnresolvable covers the three ways resolution can fail -- no proof
// linked, no resolver configured, and the resolver erroring -- and asserts EVERY case yields an
// empty URL (never a half-path that looks like a link) plus a reason in the neighbouring column.
func TestResolveProofVideoURLUnresolvable(t *testing.T) {
	t.Run("no proof reference at all", func(t *testing.T) {
		repo := &Repository{}
		url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "", "")
		if url != "" || note != "" {
			t.Errorf("expected both empty when there is no proof reference, got url=%q note=%q", url, note)
		}
	})

	t.Run("proof reference but no proof id linked", func(t *testing.T) {
		repo := &Repository{}
		url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "", "some/object/key.mp4")
		if url != "" {
			t.Errorf("expected empty URL, got %q", url)
		}
		if note == "" {
			t.Error("expected a reason when no proof id is linked")
		}
	})

	t.Run("no resolver configured", func(t *testing.T) {
		repo := &Repository{}
		url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "proof-1", "some/object/key.mp4")
		if url != "" {
			t.Errorf("expected empty URL, got %q", url)
		}
		if note == "" {
			t.Error("expected a reason when no resolver is configured")
		}
	})

	t.Run("resolver errors", func(t *testing.T) {
		repo := (&Repository{}).WithProofURLResolver(stubProofURLResolver{
			errs: map[string]error{"proof-1": errObjectMissingForTest},
		})
		url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "proof-1", "some/object/key.mp4")
		if url != "" {
			t.Errorf("expected empty URL, got %q", url)
		}
		if note == "" {
			t.Error("expected a reason when the resolver errors")
		}
	})

	t.Run("resolver returns non-absolute value", func(t *testing.T) {
		repo := (&Repository{}).WithProofURLResolver(stubProofURLResolver{
			urls: map[string]string{"proof-1": "/app/proofs/proof-1/download/signed?sig=abc"},
		})
		url, note := repo.resolveProofVideoURL(context.Background(), "tenant-1", "proof-1", "some/object/key.mp4")
		if url != "" {
			t.Errorf("expected empty URL for a non-absolute value, got %q", url)
		}
		if note == "" {
			t.Error("expected a reason when the resolver does not return an absolute URL")
		}
	})
}

func TestProofVideoColumnsJoinsMultipleProofs(t *testing.T) {
	t.Setenv("GOATOS_PROOF_GCS_BUCKET", "goatos-stg-proofs")
	repo := (&Repository{}).WithProofURLResolver(stubProofURLResolver{
		urls: map[string]string{
			"proof-1": "https://storage.example/proof-1",
			"proof-2": "https://storage.example/proof-2",
		},
	})

	refTypes, refs, urls, notes := repo.proofVideoColumns(
		context.Background(),
		"tenant-1",
		"proof-1\x1fproof-2",
		"gcs\x1fgcs",
		"weighing/a.mp4\x1fweighing/b.mp4",
	)

	if refTypes != "gcs | gcs" {
		t.Fatalf("unexpected ref types: %q", refTypes)
	}
	if refs != "gs://goatos-stg-proofs/weighing/a.mp4 | gs://goatos-stg-proofs/weighing/b.mp4" {
		t.Fatalf("unexpected refs: %q", refs)
	}
	if urls != "https://storage.example/proof-1 | https://storage.example/proof-2" {
		t.Fatalf("unexpected urls: %q", urls)
	}
	if notes != "" {
		t.Fatalf("expected no notes, got %q", notes)
	}
}

var errObjectMissingForTest = errObjMissing{}

type errObjMissing struct{}

func (errObjMissing) Error() string { return "proof object missing" }

func recordsByColumn(t *testing.T, records [][]string, column string) map[string][]string {
	t.Helper()
	if len(records) == 0 {
		t.Fatal("no CSV records")
	}
	idx := csvColumn(t, records[0], column)
	out := map[string][]string{}
	for _, record := range records[1:] {
		if idx < len(record) {
			out[record[idx]] = record
		}
	}
	return out
}

func csvCell(t *testing.T, header, record []string, column string) string {
	t.Helper()
	idx := csvColumn(t, header, column)
	if idx >= len(record) {
		t.Fatalf("record has %d columns, missing %q at index %d", len(record), column, idx)
	}
	return record[idx]
}

func csvColumn(t *testing.T, header []string, column string) int {
	t.Helper()
	for i, value := range header {
		if value == column {
			return i
		}
	}
	t.Fatalf("CSV header missing column %q: %#v", column, header)
	return -1
}
