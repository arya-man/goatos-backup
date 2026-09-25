package app

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

const (
	tenant   = "11111111-1111-4111-8111-111111111111"
	raiser   = "33333333-3333-4333-8333-333333333333"
	assignee = "44444444-4444-4444-8444-444444444444"
	taskID   = "22222222-2222-4222-8222-222222222222"
)

type fakeRepo struct {
	raised  []ports.RaiseParams
	task    domain.Task
	seen    int
	getErr  error
	unseenN int

	mentionable []domain.MentionableUser
	comments    []ports.CommentParams
}

func (f *fakeRepo) ListAssignees(context.Context, string) ([]ports.Assignee, error) { return nil, nil }
func (f *fakeRepo) ListMentionableUsers(context.Context, string, string) ([]domain.MentionableUser, error) {
	return f.mentionable, nil
}
func (f *fakeRepo) ListTasks(context.Context, ports.ListParams) (ports.Page, error) {
	return ports.Page{}, nil
}
func (f *fakeRepo) GetTask(context.Context, string, string) (domain.Task, error) {
	return f.task, f.getErr
}
func (f *fakeRepo) PeekTask(context.Context, string, string) (domain.Task, error) {
	return f.task, f.getErr
}
func (f *fakeRepo) ActivityPage(context.Context, string, string, string) (ports.ActivityPage, error) {
	return ports.ActivityPage{}, nil
}
func (f *fakeRepo) Raise(_ context.Context, p ports.RaiseParams) (domain.Task, error) {
	f.raised = append(f.raised, p)
	return domain.Task{TaskID: taskID, Attachments: p.Attachments}, nil
}
func (f *fakeRepo) Edit(context.Context, ports.EditParams) (domain.Task, error) { return f.task, nil }
func (f *fakeRepo) ChangeStatus(context.Context, ports.StatusParams) (domain.Task, error) {
	return f.task, nil
}
func (f *fakeRepo) SetComment(context.Context, ports.CommentParams) (domain.Task, error) {
	return f.task, nil
}
func (f *fakeRepo) MarkSeen(context.Context, string, string, string) (domain.Task, error) {
	f.seen++
	return f.task, nil
}
func (f *fakeRepo) UnseenCount(context.Context, string, string) (int, error) { return f.unseenN, nil }

type fakeResolver struct {
	err error
}

func (f *fakeResolver) ResolveAttachments(_ context.Context, _, _ string, refs []domain.AttachmentRef) ([]domain.Attachment, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]domain.Attachment, 0, len(refs))
	for i, r := range refs {
		out = append(out, domain.Attachment{ProofID: r.ProofID, Kind: r.Kind, MimeType: "audio/mp4", SizeBytes: 1234, Position: i})
	}
	return out, nil
}

type fakeDownloader struct {
	url   string
	calls []string
	proof proofdomain.Artifact
}

func (f *fakeDownloader) DownloadArtifact(_ context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error) {
	f.calls = append(f.calls, tenantID+"/"+proofID)
	proof := f.proof
	if proof.ProofID == "" {
		proof.ProofID = proofID
	}
	return proof, f.url, nil
}

func TestRaiseResolvesAttachmentsBeforeTheWriteAndRefusesSelfAssignment(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &fakeResolver{})
	ctx := context.Background()
	if _, err := svc.Raise(ctx, ports.RaiseParams{TenantID: tenant, ActorID: raiser, AssigneeUserID: raiser, Title: "x", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrSelfAssignment) {
		t.Fatalf("self assignment: %v", err)
	}
	if _, err := svc.Raise(ctx, ports.RaiseParams{TenantID: tenant, ActorID: raiser, AssigneeUserID: assignee, Title: "x"}); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("missing key: %v", err)
	}
	deadline := time.Now().Add(48 * time.Hour)
	task, err := svc.Raise(ctx, ports.RaiseParams{
		TenantID: tenant, ActorID: raiser, AssigneeUserID: assignee, Title: " Call the vendor ", IdempotencyKey: "k",
		Refs:       []domain.AttachmentRef{{ProofID: "p1", Kind: domain.AttachmentAudio, FileName: "note.m4a"}},
		DeadlineAt: &deadline,
	})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if len(repo.raised) != 1 || repo.raised[0].Title != "Call the vendor" {
		t.Fatalf("raise must trim and forward: %+v", repo.raised)
	}
	if len(task.Attachments) != 1 || task.Attachments[0].MimeType != "audio/mp4" || task.Attachments[0].SizeBytes != 1234 {
		t.Fatalf("attachment facts must come from the resolver, not the client: %+v", task.Attachments)
	}
	// A dead attachment refuses the whole raise before anything is written.
	repo2 := &fakeRepo{}
	svc2 := NewService(repo2, &fakeResolver{err: ports.ErrInvalidAttachment})
	if _, err := svc2.Raise(ctx, ports.RaiseParams{
		TenantID: tenant, ActorID: raiser, AssigneeUserID: assignee, Title: "x", IdempotencyKey: "k",
		Refs:       []domain.AttachmentRef{{ProofID: "p1", Kind: domain.AttachmentFile}},
		DeadlineAt: &deadline,
	}); !errors.Is(err, ports.ErrInvalidAttachment) {
		t.Fatalf("invalid attachment: %v", err)
	}
	if len(repo2.raised) != 0 {
		t.Fatal("nothing may be written when an attachment does not resolve")
	}
}

func TestGetTaskAllowsPartyOrTeamProgressMonitor(t *testing.T) {
	repo := &fakeRepo{task: domain.Task{TaskID: taskID, RaisedByUserID: raiser, AssigneeUserID: assignee, Status: domain.StatusOpen}}
	svc := NewService(repo, nil)
	ctx := context.Background()
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: "55555555-5555-4555-8555-555555555555"}, taskID); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("stranger must read not-found, got %v", err)
	}
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: raiser}, taskID); err != nil {
		t.Fatalf("raiser: %v", err)
	}
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: assignee}, taskID); err != nil {
		t.Fatalf("assignee: %v", err)
	}
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: "66666666-6666-4666-8666-666666666666", CanMonitor: true}, taskID); err != nil {
		t.Fatalf("team progress monitor: %v", err)
	}
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: assignee}, "not-a-uuid"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("bad id must read not-found, got %v", err)
	}
}

func TestAttachmentDownloadURLIsReaderAndAttachmentScoped(t *testing.T) {
	const proofID = "66666666-6666-4666-8666-666666666666"
	repo := &fakeRepo{task: domain.Task{
		TaskID:         taskID,
		RaisedByUserID: raiser,
		AssigneeUserID: assignee,
		Status:         domain.StatusOpen,
		Attachments:    []domain.Attachment{{ProofID: proofID}},
	}}
	downloader := &fakeDownloader{url: "https://media.example/download"}
	svc := NewService(repo, nil).WithAttachmentDownloader(downloader)
	ctx := context.Background()

	download, err := svc.AttachmentDownloadURL(ctx, tenant, domain.Actor{UserID: assignee}, taskID, proofID)
	if err != nil || download.URL != downloader.url || download.Artifact.ProofID != proofID {
		t.Fatalf("assignee attached proof download = %+v, %v", download, err)
	}
	if len(downloader.calls) != 1 || downloader.calls[0] != tenant+"/"+proofID {
		t.Fatalf("downloader calls = %+v", downloader.calls)
	}
	if _, err := svc.AttachmentDownloadURL(ctx, tenant, domain.Actor{UserID: assignee}, taskID, "77777777-7777-4777-8777-777777777777"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("unattached proof must read not-found, got %v", err)
	}
	if _, err := svc.AttachmentDownloadURL(ctx, tenant, domain.Actor{UserID: "55555555-5555-4555-8555-555555555555"}, taskID, proofID); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("unauthorized proof download must read not-found, got %v", err)
	}
	if download, err := svc.AttachmentDownloadURL(ctx, tenant, domain.Actor{UserID: "66666666-6666-4666-8666-666666666666", CanMonitor: true}, taskID, proofID); err != nil || download.URL != downloader.url {
		t.Fatalf("team progress monitor proof download = %+v, %v", download, err)
	}
	if len(downloader.calls) != 2 {
		t.Fatalf("downloader must not be reached for refused requests: %+v", downloader.calls)
	}
}

// Seen is stamped by the ASSIGNEE only, and only once: the raiser reading their own task
// is not "seen by the CXO", and a second open writes nothing.
func TestMarkSeenIsAssigneeOnlyAndOnce(t *testing.T) {
	repo := &fakeRepo{task: domain.Task{TaskID: taskID, RaisedByUserID: raiser, AssigneeUserID: assignee, Status: domain.StatusOpen}}
	svc := NewService(repo, nil)
	ctx := context.Background()
	if _, err := svc.MarkSeen(ctx, tenant, domain.Actor{UserID: raiser}, taskID); err != nil || repo.seen != 0 {
		t.Fatalf("raiser opening must not stamp seen (err=%v, seen=%d)", err, repo.seen)
	}
	if _, err := svc.MarkSeen(ctx, tenant, domain.Actor{UserID: assignee}, taskID); err != nil || repo.seen != 1 {
		t.Fatalf("assignee opening must stamp seen once (err=%v, seen=%d)", err, repo.seen)
	}
	stamped := repo.task
	now := stamped.RaisedAt
	stamped.SeenAt = &now
	repo.task = stamped
	if _, err := svc.MarkSeen(ctx, tenant, domain.Actor{UserID: assignee}, taskID); err != nil || repo.seen != 1 {
		t.Fatalf("a second open must write nothing (err=%v, seen=%d)", err, repo.seen)
	}
}

func TestModuleBadgeCountsAnswersOnlyThisModule(t *testing.T) {
	repo := &fakeRepo{unseenN: 3}
	svc := NewService(repo, nil)
	counts, err := svc.ModuleBadgeCounts(context.Background(), tenant, assignee, []string{"weighing", ModuleKey})
	if err != nil || counts[ModuleKey] != 3 || len(counts) != 1 {
		t.Fatalf("badge counts = %v (err %v)", counts, err)
	}
	counts, err = svc.ModuleBadgeCounts(context.Background(), tenant, assignee, []string{"weighing"})
	if err != nil || len(counts) != 0 {
		t.Fatalf("unasked module must answer nothing: %v (err %v)", counts, err)
	}
}

// The task form must set a deadline, and one behind the service clock is refused before
// anything is written; the Work Board flag, which has no form, may raise without one.
func TestRaiseRequiresADeadlineUnlessTheRaiseHasNoForm(t *testing.T) {
	repo := &fakeRepo{}
	now := time.Date(2026, 9, 14, 3, 30, 0, 0, time.UTC)
	svc := NewService(repo, &fakeResolver{}).WithClock(func() time.Time { return now })
	ctx := context.Background()
	base := ports.RaiseParams{TenantID: tenant, ActorID: raiser, AssigneeUserID: assignee, Title: "x", IdempotencyKey: "k"}

	if _, err := svc.Raise(ctx, base); !errors.Is(err, domain.ErrDeadlineRequired) {
		t.Fatalf("form raise without deadline: %v", err)
	}
	past := now.Add(-time.Minute)
	withPast := base
	withPast.DeadlineAt = &past
	if _, err := svc.Raise(ctx, withPast); !errors.Is(err, domain.ErrDeadlineNotAfterRaise) {
		t.Fatalf("past deadline: %v", err)
	}
	if len(repo.raised) != 0 {
		t.Fatal("nothing may be written when the deadline is refused")
	}

	flag := base
	flag.DeadlineOptional = true
	if _, err := svc.Raise(ctx, flag); err != nil {
		t.Fatalf("flag raise without deadline: %v", err)
	}
	future := now.Add(time.Hour)
	withFuture := base
	withFuture.DeadlineAt = &future
	if _, err := svc.Raise(ctx, withFuture); err != nil {
		t.Fatalf("future deadline: %v", err)
	}
	if len(repo.raised) != 2 || repo.raised[1].DeadlineAt == nil || !repo.raised[1].DeadlineAt.Equal(future) {
		t.Fatalf("deadline must be forwarded to the write: %+v", repo.raised)
	}
}

// TestListParamsValidatesTheWorklistFiltersAndIgnoresPinnedPeople pins the error CODES the
// screen reads and the one filter rule that is deliberately forgiving: a person filter the
// active scope already pins is IGNORED, not refused, because the filter bar is shared across
// the tabs and switching tab must never error.
func TestListParamsValidatesTheWorklistFiltersAndIgnoresPinnedPeople(t *testing.T) {
	service := NewService(&fakeRepo{}, &fakeResolver{})
	actor := domain.Actor{UserID: raiser, CanRaise: true, CanMonitor: true}
	base := ListRequest{TenantID: tenant, UserID: raiser, Actor: actor, ScopeKey: domain.ScopeTeamProgress}
	with := func(mutate func(*ListRequest)) ListRequest {
		req := base
		mutate(&req)
		return req
	}

	for _, tc := range []struct {
		name string
		req  ListRequest
		code string
	}{
		{"unknown sort", with(func(r *ListRequest) { r.Sort = "title_asc" }), "invalid_sort"},
		{"bad assignee uuid", with(func(r *ListRequest) { r.AssigneeUserID = "not-a-uuid" }), "invalid_filter"},
		{"bad raiser uuid", with(func(r *ListRequest) { r.RaisedBy = "17" }), "invalid_filter"},
		{"half-open deadline range", with(func(r *ListRequest) { r.DeadlineFrom = "2026-09-01" }), "invalid_date_range"},
		{"half-open raise range", with(func(r *ListRequest) { r.RaisedTo = "2026-09-01" }), "invalid_date_range"},
		{"inverted range", with(func(r *ListRequest) { r.RaisedFrom, r.RaisedTo = "2026-09-09", "2026-09-01" }), "invalid_date_range"},
		{"unparseable date", with(func(r *ListRequest) { r.DeadlineFrom, r.DeadlineTo = "last tuesday", "2026-09-01" }), "invalid_date_range"},
		{"over-long text", with(func(r *ListRequest) { r.Query = strings.Repeat("a", ports.MaxQueryLen+1) }), "invalid_query"},
	} {
		_, err := service.listParams(tc.req)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != tc.code {
			t.Fatalf("%s: err = %v, want code %q", tc.name, err, tc.code)
		}
	}

	// assigned_to_me pins the assignee, so an assignee filter falls away; the raiser filter stays.
	toMe, err := service.listParams(with(func(r *ListRequest) {
		r.ScopeKey, r.AssigneeUserID, r.RaisedBy = domain.ScopeAssignedToMe, assignee, raiser
	}))
	if err != nil || toMe.AssigneeUserID != "" || toMe.RaisedBy != raiser {
		t.Fatalf("assigned_to_me params = %+v err %v (the pinned assignee filter must be dropped, not refused)", toMe, err)
	}
	// assigned_by_me pins the raiser, the mirror image.
	byMe, err := service.listParams(with(func(r *ListRequest) {
		r.ScopeKey, r.AssigneeUserID, r.RaisedBy = domain.ScopeAssignedByMe, assignee, raiser
	}))
	if err != nil || byMe.RaisedBy != "" || byMe.AssigneeUserID != assignee {
		t.Fatalf("assigned_by_me params = %+v err %v", byMe, err)
	}

	// A bare integer carries the task-number arm; other text does not. A bare date range is
	// INCLUSIVE of the whole upper day, so one day named twice is a full day, not an empty range.
	numeric, err := service.listParams(with(func(r *ListRequest) {
		r.Query, r.DeadlineFrom, r.DeadlineTo = " 15 ", "2026-09-01", "2026-09-01"
	}))
	if err != nil {
		t.Fatalf("numeric query: %v", err)
	}
	if numeric.Query != "15" || numeric.QueryTaskNo == nil || *numeric.QueryTaskNo != 15 {
		t.Fatalf("query params = %+v, want the trimmed text and task_no 15", numeric)
	}
	if numeric.DeadlineFrom == nil || numeric.DeadlineTo == nil || !numeric.DeadlineTo.After(numeric.DeadlineFrom.Add(23*time.Hour)) {
		t.Fatalf("one-day range = %v..%v, want the whole IST day", numeric.DeadlineFrom, numeric.DeadlineTo)
	}
	text, err := service.listParams(with(func(r *ListRequest) { r.Query = "vendor 15" }))
	if err != nil || text.QueryTaskNo != nil || text.Sort != ports.SortRaisedAtDesc {
		t.Fatalf("text query = %+v err %v, want no task_no arm and the default sort", text, err)
	}
}

// TestBareDateRangesAreIndiaBusinessDays pins the 2026-09-25 finding with the live instants: tasks
// raised 02:55-03:01 IST on 16/09 (21:25-21:31 UTC on 15/09) were EXCLUDED from
// raised_from=raised_to=2026-09-16 and INCLUDED under 15/09, because a bare date was read as a UTC
// day. A Goat OS business day is an Asia/Kolkata day; UTC must never define it.
func TestBareDateRangesAreIndiaBusinessDays(t *testing.T) {
	service := NewService(&fakeRepo{}, &fakeResolver{})
	actor := domain.Actor{UserID: raiser, CanRaise: true, CanMonitor: true}
	ist := biztime.DefaultLocation()
	raisedEarly := time.Date(2026, 9, 16, 2, 55, 0, 0, ist)
	raisedLate := time.Date(2026, 9, 16, 3, 1, 0, 0, ist)
	lateNight := time.Date(2026, 9, 16, 23, 59, 59, 0, ist)
	nextMidnight := time.Date(2026, 9, 17, 0, 0, 0, 0, ist)

	inRange := func(p ports.ListParams, from, to *time.Time, at time.Time) bool {
		return !at.Before(*from) && !at.After(*to)
	}
	for _, field := range []string{"raised", "deadline"} {
		req := ListRequest{TenantID: tenant, UserID: raiser, Actor: actor, ScopeKey: domain.ScopeTeamProgress}
		if field == "raised" {
			req.RaisedFrom, req.RaisedTo = "2026-09-16", "2026-09-16"
		} else {
			req.DeadlineFrom, req.DeadlineTo = "2026-09-16", "2026-09-16"
		}
		p, err := service.listParams(req)
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		from, to := p.RaisedFrom, p.RaisedTo
		if field == "deadline" {
			from, to = p.DeadlineFrom, p.DeadlineTo
		}
		for _, at := range []time.Time{raisedEarly, raisedLate, lateNight} {
			if !inRange(p, from, to, at) {
				t.Fatalf("%s 16/09: %s IST is outside %s..%s -- the IST day must include it", field,
					at.Format(time.RFC3339), from.In(ist).Format(time.RFC3339Nano), to.In(ist).Format(time.RFC3339Nano))
			}
		}
		if inRange(p, from, to, nextMidnight) {
			t.Fatalf("%s 16/09 includes 17/09 00:00 IST (%s..%s)", field, from, to)
		}
		if !from.Equal(time.Date(2026, 9, 16, 0, 0, 0, 0, ist)) {
			t.Fatalf("%s lower bound = %s, want the start of 16/09 IST", field, from.In(ist))
		}
		// The previous IST day must NOT contain them.
		prev := ListRequest{TenantID: tenant, UserID: raiser, Actor: actor, ScopeKey: domain.ScopeTeamProgress,
			RaisedFrom: "2026-09-15", RaisedTo: "2026-09-15"}
		pp, err := service.listParams(prev)
		if err != nil {
			t.Fatal(err)
		}
		if inRange(pp, pp.RaisedFrom, pp.RaisedTo, raisedEarly) {
			t.Fatalf("raised 15/09 includes %s IST, which is 16/09 in India", raisedEarly)
		}
	}
}

// A refused NOTE must say it is about writing a note -- not "Only the person this task is for can
// update its status", which is what a non-party got for a comment (2026-09-25).
func TestCommentRefusalSpeaksAboutNotesNotStatus(t *testing.T) {
	var appErr *Error
	if !errors.As(HTTPError(domain.ErrNotOnTask), &appErr) {
		t.Fatalf("ErrNotOnTask did not map to an app error")
	}
	if appErr.Code != "not_on_task" || strings.Contains(strings.ToLower(appErr.Message), "status") ||
		!strings.Contains(strings.ToLower(appErr.Message), "note") {
		t.Fatalf("comment refusal = %s %q, want not_on_task and a sentence about notes", appErr.Code, appErr.Message)
	}
}

// An unknown filter key is REFUSED rather than silently widened to All (2026-09-25:
// filter=cancelled used to return every working task). Blank still means the default.
func TestUnknownFilterKeyIsRefused(t *testing.T) {
	service := NewService(&fakeRepo{}, &fakeResolver{})
	actor := domain.Actor{UserID: raiser, CanRaise: true, CanMonitor: true}
	_, err := service.listParams(ListRequest{TenantID: tenant, UserID: raiser, Actor: actor, FilterKey: "canceled"})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_filter" {
		t.Fatalf("unknown filter key: err = %v, want invalid_filter", err)
	}
	p, err := service.listParams(ListRequest{TenantID: tenant, UserID: raiser, Actor: actor, FilterKey: "cancelled"})
	if err != nil || len(p.Statuses) != 1 || p.Statuses[0] != domain.StatusCancelled {
		t.Fatalf("filter=cancelled params = %+v err %v, want statuses [cancelled]", p.Statuses, err)
	}
	if p, err := service.listParams(ListRequest{TenantID: tenant, UserID: raiser, Actor: actor}); err != nil || len(p.Statuses) != 3 {
		t.Fatalf("blank filter = %+v err %v, want the All statuses", p.Statuses, err)
	}
}
