package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

type applyCancelRepo struct {
	rows         []domain.ImportRow
	status       string
	creates      int
	appliedMoves int
}

func (r *applyCancelRepo) ReferenceLists(context.Context, string) ([]domain.ReferenceList, error) {
	return nil, nil
}
func (r *applyCancelRepo) Counts(context.Context, string) (map[string]int, error) { return nil, nil }
func (r *applyCancelRepo) List(context.Context, string, string, ports.ListParams) (ports.Page, error) {
	return ports.Page{}, nil
}
func (r *applyCancelRepo) Get(context.Context, string, string, string) (domain.Row, error) {
	return domain.Row{}, ports.ErrNotFound
}
func (r *applyCancelRepo) Options(context.Context, string, string) ([]ports.RefOption, error) {
	return nil, nil
}
func (r *applyCancelRepo) Usage(context.Context, string, string, string) (domain.Usage, error) {
	return domain.Usage{}, nil
}
func (r *applyCancelRepo) Create(_ context.Context, _ ports.WriteParams, register string, fields map[string]any) (domain.Row, error) {
	r.creates++
	return domain.Row{ID: domain.FieldString(fields, "code"), Register: register, Display: domain.FieldString(fields, "name")}, nil
}
func (r *applyCancelRepo) Update(context.Context, ports.WriteParams, string, string, map[string]any, int) (domain.Row, error) {
	return domain.Row{}, nil
}
func (r *applyCancelRepo) SetStatus(context.Context, ports.WriteParams, string, string, string, int) (domain.Row, error) {
	return domain.Row{}, nil
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
	if afterRowNo > 0 {
		return nil, nil
	}
	return r.rows, nil
}
func (r *applyCancelRepo) ImportRows(context.Context, string, string, ports.ImportRowsParams) ([]domain.ImportRow, error) {
	return nil, nil
}
func (r *applyCancelRepo) UpdateImportRows(_ context.Context, _, _, fromState string, updates []ports.ImportRowUpdate) (int, error) {
	if fromState == domain.ImportRowValid && len(updates) == 1 && updates[0].State == domain.ImportRowApplying && updates[0].RowNo == 2 {
		r.status = domain.ImportCancelled
	}
	if fromState == domain.ImportRowApplying && len(updates) == 1 && updates[0].State == domain.ImportRowApplied {
		r.appliedMoves++
	}
	return 1, nil
}
func (r *applyCancelRepo) PatchImportJob(_ context.Context, _, _ string, patch ports.ImportJobPatch) (domain.ImportJob, error) {
	if patch.ProgressRowNo != nil && *patch.ProgressRowNo == 2 {
		r.status = domain.ImportCancelled
	}
	return domain.ImportJob{Status: r.status}, nil
}
func (r *applyCancelRepo) RequestImportApply(context.Context, string, string, string) (domain.ImportJob, bool, error) {
	return domain.ImportJob{}, false, nil
}
func (r *applyCancelRepo) CancelImportJob(context.Context, string, string) (domain.ImportJob, bool, error) {
	return domain.ImportJob{}, false, nil
}

func TestApplyFinishesClaimedRowThenStopsWhenCancelled(t *testing.T) {
	repo := &applyCancelRepo{
		status: domain.ImportApplying,
		rows: []domain.ImportRow{
			{RowNo: 2, State: domain.ImportRowValid, Fields: map[string]any{"name": "One", "code": "one"}},
			{RowNo: 3, State: domain.ImportRowValid, Fields: map[string]any{"name": "Two", "code": "two"}},
		},
	}
	importer := NewImporter(NewService(repo), repo, nil, "test-worker", nil)
	err := importer.apply(context.Background(), "11111111-1111-1111-1111-111111111111", domain.ImportJob{
		ID:       "22222222-2222-2222-2222-222222222222",
		Register: domain.RegParks,
		Status:   domain.ImportApplying,
	}, domain.Registers[1])
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("creates = %d, want 1 after cancellation", repo.creates)
	}
	if repo.appliedMoves != 1 {
		t.Fatalf("applied row moves = %d, want claimed row recorded as applied", repo.appliedMoves)
	}
}
