package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/penroutines/app"
	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeService struct {
	task      domain.Task
	presence  ports.PresenceParams
	submitted ports.SubmitParams
	submitErr error
	listQuery app.ListQuery
}

func (f *fakeService) ListMine(_ context.Context, _, _ string, q app.ListQuery) (ports.Page, *domain.Tab, error) {
	f.listQuery = q
	page := ports.Page{Rows: []domain.Task{f.task}, StateCounts: map[string]int{domain.WorkStateScheduled: 1, domain.WorkStateCompleted: 2}}
	if q.TabKey == "" {
		return page, nil, nil
	}
	page.PenOptions = []ports.PenOption{{ShedID: "s1", Partition: "2", Label: "Castro 2", ParkName: "Coimbatore", Count: 3}}
	return page, &domain.Tab{Key: q.TabKey, Label: "Fumigation", Filters: []string{domain.TabFilterStatus, domain.TabFilterPen}}, nil
}
func (f *fakeService) GetTask(_ context.Context, _ string, actor domain.Actor, taskID string) (domain.Task, error) {
	if !f.task.IsAssignee(actor) || taskID != f.task.TaskID {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	return f.task, nil
}
func (f *fakeService) RecordPresence(_ context.Context, p ports.PresenceParams) (domain.Task, error) {
	f.presence = p
	t := f.task
	at := p.CapturedAt
	t.EnteredAt, t.EnteredBy = &at, p.Actor.UserID
	t.RowVersion++
	return t, nil
}
func (f *fakeService) Submit(_ context.Context, p ports.SubmitParams) (domain.Task, error) {
	f.submitted = p
	if f.submitErr != nil {
		return domain.Task{}, f.submitErr
	}
	done := f.task
	done.Status = domain.StatusPendingVerification
	done.Proofs = p.Proofs
	done.RowVersion++
	return done, nil
}
func (f *fakeService) Today() string { return "2026-09-16" }

func withActor(r *http.Request, userID string) *http.Request {
	ctx := httpmiddleware.WithTenantID(r.Context(), "tenant-1")
	ctx = httpmiddleware.WithActorID(ctx, userID)
	return r.WithContext(ctx)
}

func fixtureTask() domain.Task {
	return domain.Task{
		TaskID: "11111111-1111-4111-8111-111111111111", TenantID: "tenant-1", RoutineID: "22222222-2222-4222-8222-222222222222", RoutineVersion: 1,
		RoutineName: "Pen cleaning", Instruction: "Sweep and check the water.", ReviewKind: domain.ReviewVerifier, CadenceLine: "Every day",
		Evidence: domain.NormalizeEvidence(domain.Evidence{
			Questions: []domain.Question{{ID: "cleaned", Kind: domain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true}},
			Photo:     domain.ProofRule{Min: 1, Max: 1}, Presence: domain.PresenceRequired,
		}),
		ParkID: "p1", ParkName: "Coimbatore", ShedID: "s1", ShedName: "Castro", Partition: "2", PenLabel: "Castro 2",
		SourceDate: "2026-09-16", PlannedDate: "2026-09-16", DueDate: "2026-09-16",
		WorkState: domain.WorkStateScheduled, Status: domain.StatusOpen, AssigneeIDs: []string{"u-head"}, AssigneeNames: []string{"Park Head"}, RowVersion: 1,
	}
}

// TestListServesBackendOwnedCopy pins the wire shape the phone renders verbatim: title, filter
// chips with whole-list counts, the pen label, reason line, chip and tone, the pinned form, and
// can_check_in / can_submit from the caller's side.
func TestListServesBackendOwnedCopy(t *testing.T) {
	svc := &fakeService{task: fixtureTask()}
	h := NewHandler(svc, nil)
	rec := httptest.NewRecorder()
	h.ListMine(rec, withActor(httptest.NewRequest(http.MethodGet, "/app/pen-routines?filter=todo", nil), "u-head"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var page pagePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Title != "Routines" || len(page.Rows) != 1 || page.OpenCount != 1 || len(page.Filters) != 2 || page.Filters[1].Count != 2 {
		t.Fatalf("page = %+v", page)
	}
	row := page.Rows[0]
	if row.Title != "Pen cleaning · Castro 2 · Coimbatore" || row.PenLabel != "Castro 2" || row.ReasonLine != "Every day" || row.StateChip != "Due today" || row.StateTone != "info" || !row.CanSubmit || !row.CanCheckIn || row.InPen {
		t.Fatalf("row = %+v", row)
	}
	if len(row.Form.Questions) != 1 || row.Form.Presence != domain.PresenceRequired || row.EvidenceLine != "1 question · 1 photo · check in to pen" || row.PresenceLine != "Check in to the pen to start" {
		t.Fatalf("form = %+v evidence=%q presence=%q", row.Form, row.EvidenceLine, row.PresenceLine)
	}
	if !strings.Contains(row.Instruction, "Check in when you reach the pen") {
		t.Fatalf("instruction = %q", row.Instruction)
	}
	// A stranger's list still renders the row (the service scoped it), but their side says no.
	rec = httptest.NewRecorder()
	h.GetTask(rec, withActor(httptest.NewRequest(http.MethodGet, "/app/pen-routines/"+svc.task.TaskID, nil), "u-stranger"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status %d", rec.Code)
	}
}

// TestPresenceAndSubmitDecodeStrictlyAndRequireTheIdempotencyKey pins the write transport:
// the Idempotency-Key header is required, unknown fields are refused, the presence body's
// instant/location/integrity ride to the service, the submit body's raw answers and typed
// proof refs ride to the service, and a domain refusal maps to its stable code and farm copy.
func TestPresenceAndSubmitDecodeStrictlyAndRequireTheIdempotencyKey(t *testing.T) {
	svc := &fakeService{task: fixtureTask()}
	h := NewHandler(svc, nil)
	path := "/app/pen-routines/" + svc.task.TaskID

	// No key -> 400 missing_idempotency_key.
	rec := httptest.NewRecorder()
	r := withActor(httptest.NewRequest(http.MethodPost, path+"/presence", strings.NewReader(`{"event_type":"enter","captured_at":"2026-09-16T01:35:00Z","row_version":1}`)), "u-head")
	r.SetPathValue("task_id", svc.task.TaskID)
	h.RecordPresence(rec, r)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body.String())
	}
	// Unknown field -> 400 invalid_body.
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, path+"/presence", strings.NewReader(`{"event_type":"enter","captured_at":"2026-09-16T01:35:00Z","geofence":true}`)), "u-head")
	r.Header.Set("Idempotency-Key", "k-1")
	r.SetPathValue("task_id", svc.task.TaskID)
	h.RecordPresence(rec, r)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_body") {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}
	// A good presence punch.
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, path+"/presence", strings.NewReader(`{"event_type":"enter","captured_at":"2026-09-16T01:35:00Z","row_version":1,"location":{"latitude":11.01,"longitude":76.95,"accuracy_m":8.5,"status":"captured"},"integrity":{"mock_location":false,"device_id":"dev-1","app_version":"1.0.20"}}`)), "u-head")
	r.Header.Set("Idempotency-Key", "k-2")
	r.SetPathValue("task_id", svc.task.TaskID)
	h.RecordPresence(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("presence: %d %s", rec.Code, rec.Body.String())
	}
	if svc.presence.EventType != domain.PresenceEnter || svc.presence.IdempotencyKey != "k-2" || svc.presence.RowVersion != 1 || svc.presence.Location.Latitude == nil || *svc.presence.Location.Latitude != 11.01 || svc.presence.Integrity.DeviceID != "dev-1" || svc.presence.CapturedAt.UTC().Hour() != 1 {
		t.Fatalf("presence params = %+v", svc.presence)
	}
	var detail detailPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if !detail.Task.InPen || detail.Task.CanCheckIn || !strings.HasPrefix(detail.Task.PresenceLine, "In pen since") {
		t.Fatalf("after enter = %+v", detail.Task)
	}

	// Submit: raw answers, typed proof refs, row version.
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, path+"/submit", strings.NewReader(`{"answers":{"cleaned":"yes"},"proof_refs":[{"ref":"33333333-3333-4333-8333-333333333333","kind":"photo"}],"row_version":2,"captured_at":"2026-09-16T01:50:00Z"}`)), "u-head")
	r.Header.Set("Idempotency-Key", "k-3")
	r.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	if string(svc.submitted.Answers["cleaned"]) != `"yes"` || len(svc.submitted.Proofs) != 1 || svc.submitted.Proofs[0].Kind != domain.ProofKindPhoto || svc.submitted.RowVersion != 2 || svc.submitted.IdempotencyKey != "k-3" {
		t.Fatalf("submit params = %+v", svc.submitted)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Task.Status != domain.StatusPendingVerification || detail.Task.StateChip != "In review" || detail.Task.CanSubmit {
		t.Fatalf("after submit = %+v", detail.Task)
	}

	// A domain refusal maps to its stable code and farm copy.
	svc.submitErr = domain.ErrPresenceMissing
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, path+"/submit", strings.NewReader(`{"answers":{},"proof_refs":[]}`)), "u-head")
	r.Header.Set("Idempotency-Key", "k-4")
	r.SetPathValue("task_id", svc.task.TaskID)
	h.Submit(rec, r)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"presence_missing"`) || !strings.Contains(rec.Body.String(), "Check in to the pen before submitting.") {
		t.Fatalf("presence missing: %d %s", rec.Code, rec.Body.String())
	}
	// An answer refusal carries WHICH question in farm words.
	if e := app.HTTPError(domainAnswerErr()); e.Code != "answer_invalid" || !strings.Contains(e.Message, "Was the pen cleaned?") {
		t.Fatalf("answer error = %+v", e)
	}
	// A routine for nobody is its own code, and so is a person who cannot do routines at the park;
	// an unknown role stays invalid_routine with the reason.
	if e := app.HTTPError(domain.ValidateDefinition(domain.Definition{ParkID: "p", Name: "x", ScopeKind: domain.ScopeAllPens, CadenceKind: domain.CadenceDaily, StartDate: "2026-09-16", NotifyTime: "07:00", ReviewKind: domain.ReviewNone})); e.Code != "no_assignee" || e.HTTPStatus != http.StatusUnprocessableEntity || e.Message != "Choose who the routine is for." {
		t.Fatalf("no assignee error = %+v", e)
	}
	if e := app.HTTPError(domain.ErrNotAssignable); e.Code != "not_assignable" || e.HTTPStatus != http.StatusUnprocessableEntity {
		t.Fatalf("not assignable error = %+v", e)
	}
	if e := app.HTTPError(domain.ValidateDefinition(domain.Definition{ParkID: "p", Name: "x", ScopeKind: domain.ScopeAllPens, CadenceKind: domain.CadenceDaily, StartDate: "2026-09-16", NotifyTime: "07:00", ReviewKind: domain.ReviewNone, AssigneeUserID: "u-1", AssigneeRoles: []string{"operator"}})); e.Code != "invalid_routine" || !strings.Contains(e.Message, "operator") {
		t.Fatalf("unknown role error = %+v", e)
	}
}

func domainAnswerErr() error {
	ev := domain.NormalizeEvidence(domain.Evidence{Questions: []domain.Question{{ID: "cleaned", Kind: domain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true}}})
	_, err := domain.CheckAnswers(ev, map[string]json.RawMessage{})
	return err
}

type fakeAuthoring struct {
	created domain.Definition
	write   ports.WriteParams
	tab     domain.Tab
}

func (f *fakeAuthoring) List(context.Context, string, string) ([]ports.RoutineListRow, []ports.Park, error) {
	return []ports.RoutineListRow{{Definition: f.created, OpenToday: 2, Delayed: 1}}, []ports.Park{{ParkID: "p1", Name: "Coimbatore"}}, nil
}
func (f *fakeAuthoring) Get(context.Context, string, string) (domain.Definition, error) {
	return f.created, nil
}
func (f *fakeAuthoring) Catalog(context.Context, string, string) (app.Catalog, error) {
	return app.Catalog{Pens: []ports.CatalogPen{{ShedID: "s1", ShedName: "Castro", Partition: "2", Label: "Castro 2", Occupied: true}}, Roles: []ports.RoleHolders{
		{Role: domain.RoleParkHead, People: []ports.Person{{UserID: "u-head", DisplayName: "Dinakar"}}},
		{Role: domain.RolePCDirector, People: []ports.Person{{UserID: "u-pc", DisplayName: "Chandrakant"}, {UserID: "u-head", DisplayName: "Dinakar"}}},
		{Role: domain.RoleCXO, People: []ports.Person{{UserID: "u-ravi", DisplayName: "Ravi"}, {UserID: "u-aryaman", DisplayName: "Aryaman"}}},
	}}, nil
}
func (f *fakeAuthoring) Create(_ context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	f.write = w
	d.RoutineID = "22222222-2222-4222-8222-222222222222"
	d.CurrentVersion, d.RowVersion, d.Status = 1, 1, domain.StatusActive
	d.ParkName = "Coimbatore"
	// The real repository derives the roles from the chosen person's grants at the park and the
	// read resolves their name; the fake does the same for its two known people.
	switch d.AssigneeUserID {
	case "u-head":
		d.AssigneeRoles = []string{domain.RoleParkHead, domain.RolePCDirector}
		d.People = []domain.Assignee{{UserID: "u-head", DisplayName: "Dinakar", RoleKey: domain.RoleParkHead}}
	case "u-ravi":
		d.AssigneeRoles = []string{domain.RoleCXO}
		d.People = []domain.Assignee{{UserID: "u-ravi", DisplayName: "Ravi", RoleKey: domain.RoleCXO}}
	}
	f.created = d
	return d, nil
}
func (f *fakeAuthoring) Update(_ context.Context, _ ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	return d, nil
}
func (f *fakeAuthoring) SetStatus(_ context.Context, _ ports.WriteParams, _, status string, _ int) (domain.Definition, error) {
	d := f.created
	d.Status = status
	return d, nil
}
func (f *fakeAuthoring) ListTasks(context.Context, ports.ParkListParams) (ports.ParkPage, error) {
	return ports.ParkPage{Rows: []domain.Task{fixtureTask()}, Summary: ports.ParkSummary{Due: 1}}, nil
}
func (f *fakeAuthoring) ListTabs(context.Context, string) ([]domain.Tab, error) {
	return []domain.Tab{f.tab}, nil
}
func (f *fakeAuthoring) Today() string { return "2026-09-16" }

// TestAdminRoutesDecodeTheWriteBodyAndRenderBackendLines pins the authoring transport: the
// create body's defaults (occupied_only true, after_work due offset 1, notify time 07:00 left
// to the service), the row's backend-composed lines (cadence, evidence, status label), the
// catalog's vocabularies, and the Today table's assignee names.
func TestAdminRoutesDecodeTheWriteBodyAndRenderBackendLines(t *testing.T) {
	svc := &fakeAuthoring{}
	h := NewAdminHandler(svc, nil)

	rec := httptest.NewRecorder()
	r := withActor(httptest.NewRequest(http.MethodPost, "/admin/pen-routines", strings.NewReader(`{"park_id":"p1","name":"After deworming","scope_kind":"all_pens","cadence_kind":"after_work","after_work_kinds":["deworming","ticks_removal"],"review_kind":"none","evidence":{"questions":[],"photo":{"min":0,"max":1},"video":{"min":0,"max":0},"presence":"off"},"assignee_user_id":"u-head"}`)), "u-ceo")
	r.Header.Set("Idempotency-Key", "author-1")
	h.Create(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if svc.write.ActorID != "u-ceo" || svc.write.IdempotencyKey != "author-1" || !svc.created.OccupiedOnly || svc.created.DueOffsetDays != 1 || svc.created.AssigneeUserID != "u-head" || svc.created.StartDate != "" {
		t.Fatalf("create params = %+v / %+v", svc.write, svc.created)
	}
	var detail routineDetailPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Routine.CadenceLine != "The day after deworming or ticks removal" || detail.Routine.EvidenceLine != "up to 1 photo" || detail.Routine.StatusLabel != "Active" || detail.Routine.Pens == nil || detail.Routine.Weekdays == nil || detail.Routine.IntervalDays != nil || detail.Routine.People == nil {
		t.Fatalf("routine row = %+v", detail.Routine)
	}
	if len(detail.Routine.AssigneeRoles) != 2 || detail.Routine.AssigneeRoles[0] != (rolePayload{Key: "park_head", Label: "Park Head"}) || detail.Routine.AssigneeRoles[1].Label != "Preventive Care Director" {
		t.Fatalf("assignee roles = %+v", detail.Routine.AssigneeRoles)
	}
	if detail.Routine.Assignee == nil || *detail.Routine.Assignee != (personPayload{UserID: "u-head", DisplayName: "Dinakar"}) {
		t.Fatalf("assignee = %+v, want the one chosen person by name", detail.Routine.Assignee)
	}
	// A whole-park routine every 3 days from a chosen start: the write carries interval and
	// start, the row answers them back with the "Every 3 days" line and the park check-in copy.
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, "/admin/pen-routines", strings.NewReader(`{"park_id":"p1","name":"Medicine store","scope_kind":"park","cadence_kind":"every_n_days","interval_days":3,"start_date":"2026-09-14","review_kind":"none","evidence":{"questions":[],"photo":{"min":1,"max":1},"video":{"min":0,"max":0},"presence":"required"},"assignee_user_id":"u-ravi"}`)), "u-ceo")
	r.Header.Set("Idempotency-Key", "author-2")
	h.Create(rec, r)
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("park create: %d %s", rec.Code, rec.Body.String())
	}
	if detail.Routine.ScopeKind != "park" || detail.Routine.IntervalDays == nil || *detail.Routine.IntervalDays != 3 || detail.Routine.StartDate != "2026-09-14" || detail.Routine.CadenceLine != "Every 3 days" || detail.Routine.EvidenceLine != "1 photo · check in" || detail.Routine.AssigneeRoles[0].Label != "CXO" {
		t.Fatalf("park routine row = %+v", detail.Routine)
	}
	// Missing key on a write.
	rec = httptest.NewRecorder()
	r = withActor(httptest.NewRequest(http.MethodPost, "/admin/pen-routines/22222222-2222-4222-8222-222222222222/status", strings.NewReader(`{"status":"paused","row_version":1}`)), "u-ceo")
	h.SetStatus(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status without key: %d", rec.Code)
	}
	// Catalog vocabularies carry backend labels.
	rec = httptest.NewRecorder()
	h.Catalog(rec, withActor(httptest.NewRequest(http.MethodGet, "/admin/pen-routines/catalog?park_id=p1", nil), "u-ceo"))
	var cat catalogPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.WorkKinds) != len(domain.WorkKinds) || cat.WorkKinds[8].Label != "Pen move" || len(cat.QuestionKinds) != 5 || cat.Defaults.NotifyTime != "07:00" || len(cat.Pens) != 1 || cat.Pens[0].Display != "Castro 2" {
		t.Fatalf("catalog = %+v", cat)
	}
	// "Who does it" is one list of people, each once with every title they hold here, park head
	// first and CXO last (maintainer decision 2026-09-26: pick one person, like a task).
	wantPeople := []catalogPersonPayload{
		{UserID: "u-head", DisplayName: "Dinakar", Title: "Park Head, Preventive Care Director"},
		{UserID: "u-pc", DisplayName: "Chandrakant", Title: "Preventive Care Director"},
		{UserID: "u-aryaman", DisplayName: "Aryaman", Title: "CXO"},
		{UserID: "u-ravi", DisplayName: "Ravi", Title: "CXO"},
	}
	if len(cat.People) != len(wantPeople) {
		t.Fatalf("catalog people = %+v, want %+v", cat.People, wantPeople)
	}
	for i := range wantPeople {
		if cat.People[i] != wantPeople[i] {
			t.Fatalf("catalog people = %+v, want %+v", cat.People, wantPeople)
		}
	}
	if len(cat.QuestionProofKinds) != 1 || cat.QuestionProofKinds[0] != (optionPayload{Key: "none", Label: "No proof"}) {
		t.Fatalf("question proof authoring must be hidden until mobile rollout is enabled: %+v", cat.QuestionProofKinds)
	}
	if len(cat.QuestionProofCounts) != 2 || cat.QuestionProofCounts[0].Key != domain.QuestionProofSingle || cat.QuestionProofCounts[1].Key != domain.QuestionProofMultiple {
		t.Fatalf("question proof counts = %+v", cat.QuestionProofCounts)
	}
	if len(cat.Roles) != 3 || cat.Roles[0].Label != "Park Head" || len(cat.Roles[0].People) != 1 || cat.Roles[2].Label != "CXO" || cat.Roles[2].People == nil ||
		cat.Defaults.StartDate != "2026-09-16" || cat.Defaults.IntervalDays != 3 {
		t.Fatalf("catalog roles/defaults = %+v / %+v", cat.Roles, cat.Defaults)
	}
	scopes := map[string]string{}
	for _, o := range cat.ScopeKinds {
		scopes[o.Key] = o.Label
	}
	cadences := map[string]string{}
	for _, o := range cat.CadenceKinds {
		cadences[o.Key] = o.Label
	}
	if scopes["park"] != "Whole park (one task)" || scopes["all_pens"] != "Every pen" || scopes["selected_pens"] != "Chosen pens" || cadences["every_n_days"] != "Every few days" {
		t.Fatalf("scope/cadence vocab = %v / %v", scopes, cadences)
	}
	// The Today table carries the step plus assignee names, never ids.
	rec = httptest.NewRecorder()
	h.ListTasks(rec, withActor(httptest.NewRequest(http.MethodGet, "/admin/pen-routines/tasks?park_id=p1", nil), "u-ceo"))
	var tasks taskListPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks.Rows) != 1 || tasks.Rows[0].Title != "Pen cleaning · Castro 2 · Coimbatore" || len(tasks.Rows[0].AssigneeNames) != 1 || tasks.Rows[0].AssigneeNames[0] != "Park Head" || tasks.Summary.Due != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestAdminCatalogEnablesQuestionProofAuthoringOnlyWithRolloutFlag(t *testing.T) {
	t.Setenv("GOATOS_PEN_ROUTINE_QUESTION_PROOF_AUTHORING", "true")
	h := NewAdminHandler(&fakeAuthoring{}, nil)

	rec := httptest.NewRecorder()
	h.Catalog(rec, withActor(httptest.NewRequest(http.MethodGet, "/admin/pen-routines/catalog?park_id=p1", nil), "u-ceo"))
	var cat catalogPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, o := range cat.QuestionProofKinds {
		got[o.Key] = o.Label
	}
	if len(got) != 4 || got["none"] != "No proof" || got[domain.QuestionProofPhoto] != "Photo" || got[domain.QuestionProofVideo] != "Video" || got[domain.QuestionProofPhotoOrVideo] != "Photo or video" {
		t.Fatalf("question proof kinds = %+v", cat.QuestionProofKinds)
	}
}

// TestListOpenedFromAPhoneTabNarrowsAndNamesTheTab pins the phone-tab list contract: the tab,
// date window and pen parameters reach the service parsed, the page is titled with the TAB's
// label, and the tab's filters and pen options ride the response for the screen to render.
func TestListOpenedFromAPhoneTabNarrowsAndNamesTheTab(t *testing.T) {
	svc := &fakeService{task: fixtureTask()}
	h := NewHandler(svc, nil)
	rec := httptest.NewRecorder()
	h.ListMine(rec, withActor(httptest.NewRequest(http.MethodGet, "/app/pen-routines?tab=fumigation&due_from=2026-10-01&due_to=2026-10-08&pen=s1%7C2", nil), "u-head"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	q := svc.listQuery
	if q.TabKey != "fumigation" || q.DueFrom != "2026-10-01" || q.DueTo != "2026-10-08" || q.PenShedID != "s1" || q.PenPartition != "2" {
		t.Fatalf("query not parsed: %+v", q)
	}
	var body struct {
		Title string `json:"title"`
		Tab   *struct {
			Key     string   `json:"key"`
			Filters []string `json:"filters"`
		} `json:"tab"`
		PenOptions []struct {
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"pen_options"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "Fumigation" || body.Tab == nil || body.Tab.Key != "fumigation" || len(body.Tab.Filters) != 2 {
		t.Fatalf("tab not rendered: %+v", body)
	}
	if len(body.PenOptions) != 1 || body.PenOptions[0].Value != "s1|2" || body.PenOptions[0].Label != "Castro 2" {
		t.Fatalf("pen options = %+v", body.PenOptions)
	}

	// The Routines tab (no tab parameter) carries no tab and an empty pen list, never null.
	rec = httptest.NewRecorder()
	h.ListMine(rec, withActor(httptest.NewRequest(http.MethodGet, "/app/pen-routines", nil), "u-head"))
	if strings.Contains(rec.Body.String(), `"tab":`) || !strings.Contains(rec.Body.String(), `"pen_options":[]`) {
		t.Fatalf("routines tab body = %s", rec.Body.String())
	}
}

// TestTabListServesTheVocabulary pins the read the module SOP editor uses: the tabs (each derived
// from a phone-task SOP) and the closed module / icon / filter vocabularies, rendered verbatim.
func TestTabListServesTheVocabulary(t *testing.T) {
	svc := &fakeAuthoring{tab: domain.Tab{TabID: "33333333-3333-4333-8333-333333333333", Key: "pc_care_wash", Label: "Pen wash", ModuleKey: "pc_care", IconKey: "fumigation", Status: domain.TabStatusActive}}
	h := NewAdminHandler(svc, nil)
	rec := httptest.NewRecorder()
	h.ListTabs(rec, withActor(httptest.NewRequest(http.MethodGet, "/admin/pen-routines/tabs", nil), "u-ravi"))
	for _, want := range []string{`"module_label":"Preventive Care"`, `"modules":[{"key":"pen_routines"`, `{"key":"fumigation","label":"Fumigation"}`, `"filters":[{"key":"status"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("tab list lacks %s: %s", want, rec.Body.String())
		}
	}
}
