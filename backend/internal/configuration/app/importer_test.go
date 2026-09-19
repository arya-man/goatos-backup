package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

type applyCancelRepo struct {
	rows          []domain.ImportRow
	status        string
	creates       int
	updateVersion int
	appliedMoves  int
	// chunk bounds ImportRowsAfter; cancelAfterClaimOf flips the job to cancelled the moment a
	// chunk claim names that row (a cancel landing right after the claim).
	chunk              int
	cancelAfterClaimOf int
	mu                 sync.Mutex // rows of a chunk are written concurrently
}

func (r *applyCancelRepo) ReferenceLists(context.Context, string) ([]domain.ReferenceList, error) {
	return nil, nil
}
func (r *applyCancelRepo) Counts(context.Context, string) (map[string]int, error) { return nil, nil }
func (r *applyCancelRepo) List(context.Context, string, string, ports.ListParams) (ports.Page, error) {
	return ports.Page{}, nil
}
func (r *applyCancelRepo) Get(_ context.Context, _, register, id string) (domain.Row, error) {
	if id == "park-1" {
		return domain.Row{ID: id, Register: register, Display: "Old", RowVersion: 9, Fields: map[string]any{"name": "Old", "code": "old"}}, nil
	}
	return domain.Row{}, ports.ErrNotFound
}
func (r *applyCancelRepo) CategoryKind(context.Context, string, string) (string, error) {
	return "", ports.ErrNotFound
}
func (r *applyCancelRepo) Options(context.Context, string, string) ([]ports.RefOption, error) {
	return nil, nil
}
func (r *applyCancelRepo) Usage(context.Context, string, string, string) (domain.Usage, error) {
	return domain.Usage{}, nil
}
func (r *applyCancelRepo) Create(_ context.Context, _ ports.WriteParams, register string, fields map[string]any) (domain.Row, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	return domain.Row{ID: domain.FieldString(fields, "code"), Register: register, Display: domain.FieldString(fields, "name")}, nil
}
func (r *applyCancelRepo) Update(_ context.Context, _ ports.WriteParams, register, id string, fields map[string]any, rowVersion int) (domain.Row, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateVersion = rowVersion
	return domain.Row{ID: id, Register: register, Display: domain.FieldString(fields, "name")}, nil
}
func (r *applyCancelRepo) SetStatus(context.Context, ports.WriteParams, string, string, string, int) (domain.Row, error) {
	return domain.Row{}, nil
}

func TestApplyUsesDownloadedRowVersionForUpdates(t *testing.T) {
	repo := &applyCancelRepo{
		status: domain.ImportApplying,
		rows: []domain.ImportRow{{
			RowNo: 2, State: domain.ImportRowValid,
			Fields: map[string]any{domain.SheetColumnID: "park-1", domain.SheetColumnRowVersion: "9", "name": "Updated"},
		}},
	}
	importer := NewImporter(NewService(repo), repo, nil, "test-worker", nil)
	err := importer.apply(context.Background(), "11111111-1111-1111-1111-111111111111", domain.ImportJob{
		ID:       "22222222-2222-2222-2222-222222222222",
		Register: domain.RegParks,
		Status:   domain.ImportApplying,
	}, domain.Registers[1], nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if repo.updateVersion != 9 {
		t.Fatalf("row version = %d, want 9", repo.updateVersion)
	}
}
func (r *applyCancelRepo) Delete(context.Context, ports.WriteParams, string, string, int) error {
	return nil
}

func (r *applyCancelRepo) CreateImportJob(context.Context, domain.ImportJob, string) error {
	return nil
}
func (r *applyCancelRepo) StageImportRows(context.Context, string, string, []domain.ImportRow) error {
	return nil
}
func (r *applyCancelRepo) GetImportJob(context.Context, string, string) (domain.ImportJob, error) {
	return domain.ImportJob{}, nil
}
func (r *applyCancelRepo) ListImportJobs(context.Context, string, string, int) ([]domain.ImportJob, error) {
	return nil, nil
}
func (r *applyCancelRepo) ClaimImportJob(context.Context, string, string, string, time.Duration) (domain.ImportJob, bool, error) {
	return domain.ImportJob{Status: r.status}, r.status == domain.ImportApplying, nil
}
func (r *applyCancelRepo) DueImportJobIDs(context.Context, string, time.Duration, int) ([]string, error) {
	return nil, nil
}
func (r *applyCancelRepo) ImportRowsAfter(_ context.Context, _, _ string, _ []string, afterRowNo, _ int) ([]domain.ImportRow, error) {
	var out []domain.ImportRow
	for _, row := range r.rows {
		if row.RowNo > afterRowNo && row.State == domain.ImportRowValid {
			out = append(out, row)
		}
	}
	if r.chunk > 0 && len(out) > r.chunk {
		out = out[:r.chunk]
	}
	return out, nil
}
func (r *applyCancelRepo) ClaimImportRows(_ context.Context, _, _ string, rowNos []int) ([]int, error) {
	for _, n := range rowNos {
		if n == r.cancelAfterClaimOf {
			r.status = domain.ImportCancelled
		}
	}
	return rowNos, nil
}
func (r *applyCancelRepo) ImportRowResults(context.Context, string, string, []int) (map[int]ports.ImportRowResult, error) {
	return map[int]ports.ImportRowResult{}, nil
}
func (r *applyCancelRepo) CreateImportBundle(context.Context, domain.ImportBundle, string) error {
	return nil
}
func (r *applyCancelRepo) GetImportBundle(context.Context, string, string) (domain.ImportBundle, error) {
	return domain.ImportBundle{}, ports.ErrNotFound
}
func (r *applyCancelRepo) ListImportBundles(context.Context, string, int) ([]domain.ImportBundle, error) {
	return nil, nil
}
func (r *applyCancelRepo) DueImportBundleIDs(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (r *applyCancelRepo) PatchImportBundle(context.Context, string, string, ports.ImportBundlePatch) (domain.ImportBundle, error) {
	return domain.ImportBundle{}, nil
}
func (r *applyCancelRepo) RequestImportBundleApply(context.Context, string, string, string) (domain.ImportBundle, bool, error) {
	return domain.ImportBundle{}, false, nil
}
func (r *applyCancelRepo) CancelImportBundle(context.Context, string, string) (domain.ImportBundle, bool, error) {
	return domain.ImportBundle{}, false, nil
}
func (r *applyCancelRepo) PromoteImportJob(context.Context, string, string, string, string) (domain.ImportJob, bool, error) {
	return domain.ImportJob{}, false, nil
}
func (r *applyCancelRepo) ImportRows(context.Context, string, string, ports.ImportRowsParams) ([]domain.ImportRow, error) {
	return nil, nil
}
func (r *applyCancelRepo) UpdateImportRows(_ context.Context, _, _, fromState string, updates []ports.ImportRowUpdate) (int, error) {
	for _, u := range updates {
		if fromState == domain.ImportRowApplying && u.State == domain.ImportRowApplied {
			r.appliedMoves++
		}
		for i := range r.rows {
			if r.rows[i].RowNo == u.RowNo {
				r.rows[i].State = u.State
			}
		}
	}
	return len(updates), nil
}
func (r *applyCancelRepo) PatchImportJob(_ context.Context, _, _ string, patch ports.ImportJobPatch) (domain.ImportJob, error) {
	return domain.ImportJob{Status: r.status}, nil
}
func (r *applyCancelRepo) RequestImportApply(context.Context, string, string, string) (domain.ImportJob, bool, error) {
	return domain.ImportJob{}, false, nil
}
func (r *applyCancelRepo) CancelImportJob(context.Context, string, string) (domain.ImportJob, bool, error) {
	return domain.ImportJob{}, false, nil
}

// TestApplyFinishesClaimedChunkThenStopsWhenCancelled pins the cancel boundary of the chunked
// apply: rows a chunk claim moved to applying are finished (their writes are owed, the rows
// say applying), and no later chunk starts once the job is cancelled.
func TestApplyFinishesClaimedChunkThenStopsWhenCancelled(t *testing.T) {
	repo := &applyCancelRepo{
		status: domain.ImportApplying,
		chunk:  2, cancelAfterClaimOf: 2,
		rows: []domain.ImportRow{
			{RowNo: 2, State: domain.ImportRowValid, Fields: map[string]any{"name": "One", "code": "one"}},
			{RowNo: 3, State: domain.ImportRowValid, Fields: map[string]any{"name": "Two", "code": "two"}},
			{RowNo: 4, State: domain.ImportRowValid, Fields: map[string]any{"name": "Three", "code": "three"}},
		},
	}
	importer := NewImporter(NewService(repo), repo, nil, "test-worker", nil)
	err := importer.apply(context.Background(), "11111111-1111-1111-1111-111111111111", domain.ImportJob{
		ID:       "22222222-2222-2222-2222-222222222222",
		Register: domain.RegParks,
		Status:   domain.ImportApplying,
	}, domain.Registers[1], nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if repo.creates != 2 {
		t.Fatalf("creates = %d, want the claimed chunk of 2 and not the row after the cancel", repo.creates)
	}
	if repo.appliedMoves != 2 {
		t.Fatalf("applied row moves = %d, want the claimed rows recorded as applied", repo.appliedMoves)
	}
	if repo.rows[2].State != domain.ImportRowValid {
		t.Fatalf("row 4 state = %s, want untouched valid", repo.rows[2].State)
	}
}

// TestDuplicateTagIndexSpansTheWholeSheet pins the review finding of 2026-09-19: a tag repeated
// far apart on the animals sheet is caught at preview by the job-level index, on either tag
// column, ignoring case and spacing, and a rebuilt index (a resume) still knows the earlier row.
func TestDuplicateTagIndexSpansTheWholeSheet(t *testing.T) {
	seen := map[string]int{}
	first := domain.ImportRow{RowNo: 10, Fields: map[string]any{"animal_identifier_1": "IN-001", "animal_identifier_2": "T 77"}}
	if errs := duplicateTagErrors(seen, first); len(errs) != 0 {
		t.Fatalf("first row flagged: %+v", errs)
	}
	later := domain.ImportRow{RowNo: 70000, Fields: map[string]any{"animal_identifier_1": "in-001"}}
	errs := duplicateTagErrors(seen, later)
	if len(errs) != 1 || errs[0].Field != "animal_identifier_1" || errs[0].Code != "duplicate" || errs[0].Message != "This tag is already on row 10 of this sheet." {
		t.Fatalf("duplicate primary tag = %+v", errs)
	}
	crossed := domain.ImportRow{RowNo: 70001, Fields: map[string]any{"animal_identifier_1": "NEW-1", "animal_identifier_2": "t77"}}
	if errs := duplicateTagErrors(seen, crossed); len(errs) != 1 || errs[0].Field != "animal_identifier_2" {
		t.Fatalf("duplicate secondary tag = %+v", errs)
	}
	if _, filed := seen["NEW-1"]; filed {
		t.Fatal("a flagged row's other tag was filed as if the row were clean")
	}
	rebuilt := map[string]int{}
	rememberTags(rebuilt, first)
	if errs := duplicateTagErrors(rebuilt, later); len(errs) != 1 {
		t.Fatalf("rebuilt index missed the earlier row: %+v", errs)
	}
}
