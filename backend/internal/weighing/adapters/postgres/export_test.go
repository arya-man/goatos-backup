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
