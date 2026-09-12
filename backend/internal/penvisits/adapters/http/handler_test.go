package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeService struct {
	task      domain.Task
	submitted ports.SubmitParams
	submitErr error
}

func (f *fakeService) ListMine(_ context.Context, _, userID, filterKey string, limit int, _ string) (ports.Page, error) {
	return ports.Page{Rows: []domain.Task{f.task}, StateCounts: map[string]int{domain.WorkStateScheduled: 1, domain.WorkStateCompleted: 2}}, nil
}
func (f *fakeService) GetTask(_ context.Context, _ string, actor domain.Actor, taskID string) (domain.Task, error) {
	if !f.task.IsAssignee(actor) || taskID != f.task.TaskID {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	return f.task, nil
}
func (f *fakeService) Submit(_ context.Context, p ports.SubmitParams) (domain.Task, error) {
	f.submitted = p
	if f.submitErr != nil {
		return domain.Task{}, f.submitErr
	}
	done := f.task
	done.Status = domain.StatusPendingVerification
	done.ProofRef = &p.ProofRef
	done.RowVersion++
	return done, nil
}
func (f *fakeService) Today() string { return "2026-09-07" }

func withActor(r *http.Request, userID string) *http.Request {
	ctx := httpmiddleware.WithTenantID(r.Context(), "tenant-1")
	ctx = httpmiddleware.WithActorID(ctx, userID)
	return r.WithContext(ctx)
}

func fixtureTask() domain.Task {
	return domain.Task{
		TaskID: "11111111-1111-4111-8111-111111111111", TenantID: "tenant-1", ParkID: "p1", ParkName: "Coimbatore",
		ShedID: "s1", ShedName: "Castro", Partition: "2", PenLabel: "Castro 2",
		Reasons: []string{domain.ReasonVaccination}, SourceDate: "2026-09-06", PlannedDate: "2026-09-07", DueDate: "2026-09-07",
		WorkState: domain.WorkStateScheduled, Status: domain.StatusOpen, VisitorIDs: []string{"u-dinakar"}, RowVersion: 1,
	}
}

// TestListServesBackendOwnedCopy pins the wire shape the phone renders verbatim: title, filter
// chips with whole-list counts, the pen label, reason line, chip and tone, and can_submit from
// the caller's side.
func TestListServesBackendOwnedCopy(t *testing.T) {
	svc := &fakeService{task: fixtureTask()}
	h := NewHandler(svc, nil)
	rec := httptest.NewRecorder()
	h.ListMine(rec, withActor(httptest.NewRequest(http.MethodGet, "/app/pen-visits?filter=todo", nil), "u-dinakar"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var page visitPagePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Title != "Pen visits" || len(page.Rows) != 1 || page.OpenCount != 1 {
		t.Fatalf("page = %+v", page)
	}
	row := page.Rows[0]
	if row.Title != "Visit Castro 2 · Coimbatore" || row.PenLabel != "Castro 2" || row.ReasonLine != "Vaccination yesterday" || row.StateChip != "Visit pen today" || row.StateTone != "info" || !row.CanSubmit {
		t.Fatalf("row = %+v", row)
	}
	if len(page.Filters) != 2 || page.Filters[0].Key != "todo" || !page.Filters[0].Selected || page.Filters[0].Count != 1 || page.Filters[1].Count != 2 {
		t.Fatalf("filters = %+v", page.Filters)
	}
	// Someone else's read of the same task is a 404, never a 403 that confirms it exists.
	rec = httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodGet, "/app/pen-visits/"+svc.task.TaskID, nil), "u-other")
	req.SetPathValue("task_id", svc.task.TaskID)
	h.GetTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other person's detail = %d", rec.Code)
	}
}

// TestSubmitRequiresKeyAndCarriesProof pins the write contract: no Idempotency-Key is a 400
// before anything is read, an unknown field is refused, and a good call hands the proof, the
// version and the key to the service and answers with the completed visit.
func TestSubmitRequiresKeyAndCarriesProof(t *testing.T) {
	svc := &fakeService{task: fixtureTask()}
	h := NewHandler(svc, nil)
	path := "/app/pen-visits/" + svc.task.TaskID + "/submit"

	rec := httptest.NewRecorder()
	req := withActor(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"proof_ref":"22222222-2222-4222-8222-222222222222","row_version":1}`)), "u-dinakar")
	req.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
		t.Fatalf("no key = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = withActor(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"proof_ref":"x","animal_count":3}`)), "u-dinakar")
	req.Header.Set("Idempotency-Key", "k1")
	req.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_body") {
		t.Fatalf("unknown field = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = withActor(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"proof_ref":"22222222-2222-4222-8222-222222222222","row_version":1}`)), "u-dinakar")
	req.Header.Set("Idempotency-Key", "k1")
	req.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d %s", rec.Code, rec.Body.String())
	}
	if svc.submitted.ProofRef != "22222222-2222-4222-8222-222222222222" || svc.submitted.RowVersion != 1 || svc.submitted.IdempotencyKey != "k1" || svc.submitted.Actor.UserID != "u-dinakar" {
		t.Fatalf("service received %+v", svc.submitted)
	}
	var out visitDetailPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// Submit hands the clip to the verifier: the kernel clock stays open, the gate reads
	// pending_verification, and the chip says so -- never "Done" (maintainer decision 2026-09-12).
	if out.Task.WorkState != domain.WorkStateScheduled || out.Task.Status != domain.StatusPendingVerification || out.Task.StateChip != "Visit in review" || out.Task.CanSubmit || out.Task.RowVersion != 2 {
		t.Fatalf("submitted payload = %+v", out.Task)
	}

	// A proof the store cannot vouch for is a 422 the phone maps to "record again".
	svc.submitErr = domain.ErrInvalidProof
	rec = httptest.NewRecorder()
	req = withActor(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"proof_ref":"22222222-2222-4222-8222-222222222222"}`)), "u-dinakar")
	req.Header.Set("Idempotency-Key", "k2")
	req.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid_proof") {
		t.Fatalf("invalid proof = %d %s", rec.Code, rec.Body.String())
	}
}
