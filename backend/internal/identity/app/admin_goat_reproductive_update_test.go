package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// reproMatchRepo augments the base fakeRepo with the optional
// reproductiveBulkMatcher capability so the matched-existing reproductive update
// path can be exercised without a database.
type reproMatchRepo struct {
	*fakeRepo
	match        ports.ReproductiveMatchResult
	matchErr     error
	lastMatchCmd ports.ResolveReproductiveMatchCommand
}

func (r *reproMatchRepo) ResolveGoatForReproductiveBulkUpdate(_ context.Context, cmd ports.ResolveReproductiveMatchCommand) (ports.ReproductiveMatchResult, error) {
	r.lastMatchCmd = cmd
	return r.match, r.matchErr
}

func identifierAlreadyOwnedValidation(ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error) {
	return ports.AdminGoatCreateValidation{
		Conflicts: []domain.FieldError{{
			Field:   "animal_identifier_1",
			Code:    "identifier_already_owned",
			Message: "animal_identifier_1 already belongs to animal 10000000-0000-4000-8000-000000000042",
		}},
	}, nil
}

const reproUpdateCSV = "Animal ID 1,Animal ID 2,Species,Park,Shed,Sex,DOB,Origin,Management stage,Entry date,Reproductive status\n" +
	"A1-EXIST-001,A2-EXIST-001,goat,CBE,K1,female,2026-06-01,birth,K1,2026-06-15,pregnant\n"

func commitRawFromPreview(t *testing.T, preview *domain.AdminGoatBulkResponse) []byte {
	t.Helper()
	rows := make([]map[string]any, 0, len(preview.Rows))
	for _, row := range preview.Rows {
		if row.Decision != "create" && row.Decision != bulkImportUpdateReproductiveDecision {
			continue
		}
		rows = append(rows, map[string]any{"row_number": row.RowNumber, "normalized": row.Normalized})
	}
	raw, err := json.Marshal(map[string]any{
		"file_hash":     testBulkHash,
		"preview_token": preview.PreviewToken,
		"rows":          rows,
	})
	if err != nil {
		t.Fatalf("marshal commit body: %v", err)
	}
	return raw
}

func TestBulkImportMatchedExistingReproductiveUpdate(t *testing.T) {
	repo := &reproMatchRepo{
		fakeRepo: &fakeRepo{validateAdminGoatCreateFunc: identifierAlreadyOwnedValidation},
		match: ports.ReproductiveMatchResult{
			Matched:                   true,
			GoatID:                    "10000000-0000-4000-8000-000000000042",
			CurrentReproductiveStatus: "open",
			RowVersion:                7,
			MatchConfidence:           1.0,
		},
	}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())

	preview, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, reproUpdateCSV, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Summary.UpdateReady != 1 || preview.Summary.RequiresReview != 0 || preview.Summary.CreateReady != 0 {
		t.Fatalf("preview summary = %#v, want one update-ready row", preview.Summary)
	}
	row := preview.Rows[0]
	if row.Decision != bulkImportUpdateReproductiveDecision {
		t.Fatalf("preview decision = %q, want %q", row.Decision, bulkImportUpdateReproductiveDecision)
	}
	if row.MatchedGoatID == nil || *row.MatchedGoatID != "10000000-0000-4000-8000-000000000042" {
		t.Fatalf("matched_goat_id = %#v", row.MatchedGoatID)
	}
	if row.MatchConfidence == nil || *row.MatchConfidence != 1.0 {
		t.Fatalf("match_confidence = %#v", row.MatchConfidence)
	}
	if row.SourceRef == nil || *row.SourceRef == "" {
		t.Fatalf("source_ref = %#v, want the CSV source record id", row.SourceRef)
	}

	commit, err := svc.CommitAdminGoatBulkImport(context.Background(), CommitAdminGoatBulkInput{
		TenantID:       testTenant,
		ActorID:        testActor,
		IdempotencyKey: "bulk-repro-0001",
		TraceID:        testTrace,
		RawBody:        commitRawFromPreview(t, preview),
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if commit.Summary.Updated != 1 || commit.Summary.Created != 0 || commit.Summary.Failed != 0 {
		t.Fatalf("commit summary = %#v, want one reproductive update", commit.Summary)
	}
	if len(commit.Rows) != 1 || commit.Rows[0].Decision != bulkImportUpdateReproductiveDecision {
		t.Fatalf("commit row = %#v", commit.Rows)
	}
	if commit.Rows[0].Result == nil || len(commit.Rows[0].Result.Events) == 0 || commit.Rows[0].Result.Events[0].EventType != "goat.reproductive.changed" {
		t.Fatalf("commit row must carry the goat.reproductive.changed event: %#v", commit.Rows[0].Result)
	}
	// No-clobber: expected_row_version passed to the transition is the matched
	// goat's captured row_version, and no create was performed.
	if repo.fakeRepo.lastReproductiveGoatCmd.RowVersion != 7 {
		t.Fatalf("reproductive command row_version = %d, want captured 7", repo.fakeRepo.lastReproductiveGoatCmd.RowVersion)
	}
	if repo.fakeRepo.lastReproductiveGoatCmd.ReproductiveStatus != "pregnant" || repo.fakeRepo.lastReproductiveGoatCmd.GoatID != "10000000-0000-4000-8000-000000000042" {
		t.Fatalf("reproductive command = %#v", repo.fakeRepo.lastReproductiveGoatCmd)
	}
	if len(repo.fakeRepo.createAdminGoatCmds) != 0 {
		t.Fatalf("matched-existing update must not create a goat, creates=%d", len(repo.fakeRepo.createAdminGoatCmds))
	}
}

func TestBulkImportMatchedExistingReproductiveNoopStaysReview(t *testing.T) {
	repo := &reproMatchRepo{
		fakeRepo: &fakeRepo{validateAdminGoatCreateFunc: identifierAlreadyOwnedValidation},
		match: ports.ReproductiveMatchResult{
			Matched:                   true,
			GoatID:                    "10000000-0000-4000-8000-000000000042",
			CurrentReproductiveStatus: "pregnant", // already at target -> no-op
			RowVersion:                7,
			MatchConfidence:           1.0,
		},
	}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	preview, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, reproUpdateCSV, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Summary.UpdateReady != 0 || preview.Summary.RequiresReview != 1 {
		t.Fatalf("no-op preview summary = %#v, want requires_review", preview.Summary)
	}
}

// TestBulkImportReproductiveUpdateRequiresCapability proves the path is additive:
// without the matcher capability a reproductive_status row that matches an
// existing goat keeps the original requires_review behavior.
func TestBulkImportReproductiveUpdateRequiresCapability(t *testing.T) {
	repo := &fakeRepo{validateAdminGoatCreateFunc: identifierAlreadyOwnedValidation}
	svc := NewService(repo).WithBulkPreviewSigningKey(DevBulkPreviewSigningKey())
	preview, err := svc.PreviewAdminGoatBulkImport(context.Background(), PreviewAdminGoatBulkInput{
		TenantID: testTenant,
		TraceID:  testTrace,
		RawBody:  []byte(fmt.Sprintf(`{"csv":%q,"file_hash":%q}`, reproUpdateCSV, testBulkHash)),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Summary.UpdateReady != 0 || preview.Summary.RequiresReview != 1 {
		t.Fatalf("without matcher capability summary = %#v, want requires_review", preview.Summary)
	}
}
