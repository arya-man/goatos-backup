package postgres

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/app"
	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestConfigurationBulkSheetsPostgresPaths drives a sheet through the real pipeline: staged
// into configuration_import_rows, validated into a preview with per-row messages (an unknown
// park by label, an in-sheet duplicate), applied through the ordinary register write with a
// per-row idempotency key, then re-downloaded and uploaded back as an UPDATE by id. It also pins
// the two things a mid-flight cancel must not lose: the phase end cannot overwrite 'cancelled',
// and rows a cancel marked skipped are not re-marked by the chunk that was in flight.
func TestConfigurationBulkSheetsPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second)
	svc := app.NewService(repo)
	importer := app.NewImporter(svc, repo, nil, "test-worker", nil)
	w := ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, TraceID: "trace-sheet"}

	stage := func(register, name, body string) domain.ImportJob {
		t.Helper()
		job, err := importer.Stage(ctx, w, register, name, strings.NewReader(body))
		if err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
		return job
	}
	waitFor := func(jobID string, statuses ...string) domain.ImportJob {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			job, err := repo.GetImportJob(ctx, cfgTenant, jobID)
			if err != nil {
				t.Fatalf("get job: %v", err)
			}
			for _, s := range statuses {
				if job.Status == s {
					return job
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("job %s never reached %v", jobID, statuses)
		return domain.ImportJob{}
	}

	// 1. Create sheet: a good row (park by LABEL), a good row (park by CODE), an unknown park, a
	//    duplicate name, a bad number.
	job := stage(domain.RegPens, "pens.csv", "park_id,name,capacity,sex\nCoimbatore,Sheet A,40,female\nCBE,Sheet B,20,mixed\nNowhere,Sheet C,10,\nCoimbatore,Sheet A,5,\nCBE,Sheet D,abc,\n")
	job = waitFor(job.ID, domain.ImportPreviewed, domain.ImportFailed)
	if job.Status != domain.ImportPreviewed || job.TotalRows != 5 || job.ValidRows != 2 || job.InvalidRows != 3 {
		t.Fatalf("preview = %+v", job)
	}
	invalid, err := repo.ImportRows(ctx, cfgTenant, job.ID, ports.ImportRowsParams{State: domain.ImportRowInvalid, Limit: 10})
	if err != nil || len(invalid) != 3 {
		t.Fatalf("invalid rows: %v %d", err, len(invalid))
	}
	want := map[int]string{4: "park_id", 5: "name", 6: "capacity"}
	for _, row := range invalid {
		if len(row.Errors) == 0 || row.Errors[0].Field != want[row.RowNo] {
			t.Fatalf("row %d errors = %+v, want field %s", row.RowNo, row.Errors, want[row.RowNo])
		}
	}
	// Nothing is written before apply.
	if page, _ := repo.List(ctx, cfgTenant, domain.RegPens, ports.ListParams{Query: "Sheet", Limit: 10}); len(page.Rows) != 0 {
		t.Fatalf("rows written before apply: %d", len(page.Rows))
	}
	if _, ok, err := repo.RequestImportApply(ctx, cfgTenant, job.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply: %v %v", err, ok)
	}
	if err := importer.Process(ctx, cfgTenant, job.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	job = waitFor(job.ID, domain.ImportApplied, domain.ImportFailed)
	if job.Status != domain.ImportApplied || job.AppliedRows != 2 || job.FailedRows != 0 {
		t.Fatalf("applied = %+v", job)
	}
	page, err := repo.List(ctx, cfgTenant, domain.RegPens, ports.ListParams{Query: "Sheet", Limit: 10})
	if err != nil || len(page.Rows) != 2 {
		t.Fatalf("pens after apply: %v %d", err, len(page.Rows))
	}
	for _, row := range page.Rows {
		if domain.FieldString(row.Fields, "park_id") != cfgParkCBE {
			t.Fatalf("park not resolved on %s: %v", row.Display, row.Fields)
		}
	}
	// A second apply of the same job is a no-op (rows are applied, not valid).
	if _, ok, _ := repo.RequestImportApply(ctx, cfgTenant, job.ID, cfgActor); ok {
		t.Fatalf("an applied job must not re-enter applying")
	}

	// 2. Download, then upload the download back with one capacity changed: an UPDATE by id,
	//    and every other row unchanged (the same fields resend as no-ops).
	var buf bytes.Buffer
	if err := svc.Export(ctx, cfgTenant, domain.RegPens, "all", domain.FormatCSV, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.HasPrefix(lines[0], "id,row_version,park_id,name,capacity,notes") {
		t.Fatalf("export header = %q", lines[0])
	}
	edited := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, ",Sheet A,40,") {
			line = strings.Replace(line, ",Sheet A,40,", ",Sheet A,77,", 1)
		}
		edited = append(edited, line)
	}
	job2 := stage(domain.RegPens, "pens-edit.csv", strings.Join(edited, "\n")+"\n")
	job2 = waitFor(job2.ID, domain.ImportPreviewed, domain.ImportFailed)
	if job2.Status != domain.ImportPreviewed || job2.InvalidRows != 0 || job2.ValidRows != len(lines)-1 {
		t.Fatalf("edit preview = %+v (lines %d)", job2, len(lines)-1)
	}
	if _, _, err := repo.RequestImportApply(ctx, cfgTenant, job2.ID, cfgActor); err != nil {
		t.Fatal(err)
	}
	if err := importer.Process(ctx, cfgTenant, job2.ID); err != nil {
		t.Fatalf("apply edit: %v", err)
	}
	job2 = waitFor(job2.ID, domain.ImportApplied, domain.ImportFailed)
	if job2.Status != domain.ImportApplied || job2.FailedRows != 0 {
		t.Fatalf("edit applied = %+v", job2)
	}
	page, _ = repo.List(ctx, cfgTenant, domain.RegPens, ports.ListParams{Query: "Sheet A", Limit: 10})
	if len(page.Rows) != 1 {
		t.Fatalf("Sheet A rows = %d", len(page.Rows))
	}
	if cap, _ := domain.FieldInt(page.Rows[0].Fields, "capacity"); cap != 77 {
		t.Fatalf("capacity after update = %d, want 77", cap)
	}

	// 3. An update-only sheet (id + row_version + one column) is accepted without the required
	//    columns.
	job3 := stage(domain.RegPens, "cap.csv", "id,row_version,capacity\n"+page.Rows[0].ID+","+strconv.Itoa(page.Rows[0].RowVersion)+",88\n")
	job3 = waitFor(job3.ID, domain.ImportPreviewed, domain.ImportFailed)
	if job3.ValidRows != 1 {
		t.Fatalf("update-only preview = %+v", job3)
	}
	// A create sheet missing a required column is refused at upload.
	if _, err := importer.Stage(ctx, w, domain.RegPens, "bad.csv", strings.NewReader("name\nX\n")); err == nil || !strings.Contains(err.Error(), "missing_columns") {
		t.Fatalf("missing required column must refuse the upload, got %v", err)
	}
	// An XLSX round trip reads the same header.
	var xbuf bytes.Buffer
	if err := svc.Template(ctx, cfgTenant, domain.RegPens, domain.FormatXLSX, &xbuf); err != nil {
		t.Fatalf("xlsx template: %v", err)
	}
	reader, err := app.OpenSheet(domain.FormatXLSX, bytes.NewReader(xbuf.Bytes()))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	header, err := reader.Next()
	if err != nil || strings.Join(header, ",") != "id,row_version,park_id,name,capacity,notes" {
		t.Fatalf("xlsx header = %v %v", header, err)
	}

	// 4. Cancel fences: cancel a previewed job, then a stale phase end must NOT flip it back.
	job4 := stage(domain.RegPens, "cancel.csv", "park_id,name\nCBE,Sheet E\nCBE,Sheet F\n")
	job4 = waitFor(job4.ID, domain.ImportPreviewed)
	if _, ok, err := repo.CancelImportJob(ctx, cfgTenant, job4.ID); err != nil || !ok {
		t.Fatalf("cancel: %v %v", err, ok)
	}
	validating := domain.ImportValidating
	previewed := domain.ImportPreviewed
	if _, err := repo.PatchImportJob(ctx, cfgTenant, job4.ID, ports.ImportJobPatch{FromStatus: &validating, Status: &previewed}); err != ports.ErrVersionConflict {
		t.Fatalf("a fenced phase end on a cancelled job must miss, got %v", err)
	}
	if moved, err := repo.UpdateImportRows(ctx, cfgTenant, job4.ID, domain.ImportRowValid, []ports.ImportRowUpdate{{RowNo: 2, State: domain.ImportRowApplied}}); err != nil || moved != 0 {
		t.Fatal(err)
	}
	rows, _ := repo.ImportRows(ctx, cfgTenant, job4.ID, ports.ImportRowsParams{Limit: 10})
	for _, row := range rows {
		if row.State != domain.ImportRowSkipped {
			t.Fatalf("row %d = %s after cancel, want skipped", row.RowNo, row.State)
		}
	}
	got, _ := repo.GetImportJob(ctx, cfgTenant, job4.ID)
	if got.Status != domain.ImportCancelled {
		t.Fatalf("status after stale phase end = %s", got.Status)
	}

	// 5. Recovery sweep: a job whose claim lapsed is picked up by ProcessDue and finished.
	job5 := stage(domain.RegPens, "sweep.csv", "park_id,name\nCBE,Sheet G\n")
	job5 = waitFor(job5.ID, domain.ImportPreviewed)
	if _, _, err := repo.RequestImportApply(ctx, cfgTenant, job5.ID, cfgActor); err != nil {
		t.Fatal(err)
	}
	// Park it as if a worker died holding it an hour ago.
	if _, err := pool.Exec(ctx, `UPDATE configuration_import_jobs SET claimed_at = now() - interval '1 hour', claimed_by = 'dead' WHERE job_id = $1::uuid`, job5.ID); err != nil {
		t.Fatal(err)
	}
	if ids, err := repo.DueImportJobIDs(ctx, cfgTenant, 5*time.Minute, 10); err != nil || len(ids) != 1 || ids[0] != job5.ID {
		t.Fatalf("due = %v %v", ids, err)
	}
	if n, err := importer.ProcessDue(ctx, cfgTenant, 10); err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if got, _ := repo.GetImportJob(ctx, cfgTenant, job5.ID); got.Status != domain.ImportApplied || got.AppliedRows != 1 {
		t.Fatalf("swept job = %+v", got)
	}
	// 6. If cancel lands after a row was claimed for apply, the cancelled job remains recoverable
	// so the claimed row is settled through its idempotency key instead of being stranded.
	job6 := stage(domain.RegPens, "cancel-applying.csv", "park_id,name\nCBE,Sheet H\n")
	job6 = waitFor(job6.ID, domain.ImportPreviewed)
	if _, _, err := repo.RequestImportApply(ctx, cfgTenant, job6.ID, cfgActor); err != nil {
		t.Fatal(err)
	}
	if moved, err := repo.UpdateImportRows(ctx, cfgTenant, job6.ID, domain.ImportRowValid, []ports.ImportRowUpdate{{RowNo: 2, State: domain.ImportRowApplying, Fields: map[string]any{"park_id": cfgParkCBE, "name": "Sheet H"}}}); err != nil || moved != 1 {
		t.Fatalf("claim row for apply: moved=%d err=%v", moved, err)
	}
	if _, ok, err := repo.CancelImportJob(ctx, cfgTenant, job6.ID); err != nil || !ok {
		t.Fatalf("cancel applying: %v %v", err, ok)
	}
	if ids, err := repo.DueImportJobIDs(ctx, cfgTenant, 5*time.Minute, 10); err != nil || len(ids) != 1 || ids[0] != job6.ID {
		t.Fatalf("cancelled applying due = %v %v", ids, err)
	}
	if n, err := importer.ProcessDue(ctx, cfgTenant, 10); err != nil || n != 1 {
		t.Fatalf("cancelled applying sweep: %d %v", n, err)
	}
	got6, _ := repo.GetImportJob(ctx, cfgTenant, job6.ID)
	if got6.Status != domain.ImportCancelled || got6.AppliedRows != 1 || got6.FinishedAt == "" {
		t.Fatalf("cancelled applying job = %+v", got6)
	}
	rows6, _ := repo.ImportRows(ctx, cfgTenant, job6.ID, ports.ImportRowsParams{Limit: 10})
	if len(rows6) != 1 || rows6[0].State != domain.ImportRowApplied {
		t.Fatalf("cancelled applying rows = %+v", rows6)
	}
	// The error sheet carries only the rows to fix.
	var ebuf bytes.Buffer
	if err := svc.ErrorSheet(ctx, repo, cfgTenant, job.ID, domain.FormatCSV, &ebuf); err != nil {
		t.Fatalf("error sheet: %v", err)
	}
	if n := len(strings.Split(strings.TrimSpace(ebuf.String()), "\n")); n != 4 {
		t.Fatalf("error sheet lines = %d, want header + 3:\n%s", n, ebuf.String())
	}
	importer.Wait()
}
