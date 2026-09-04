package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
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
}

func (f *fakeRepo) ListAssignees(context.Context, string) ([]ports.Assignee, error) { return nil, nil }
func (f *fakeRepo) ListTasks(context.Context, ports.ListParams) (ports.Page, error) {
	return ports.Page{}, nil
}
func (f *fakeRepo) GetTask(context.Context, string, string) (domain.Task, error) {
	return f.task, f.getErr
}
func (f *fakeRepo) Raise(_ context.Context, p ports.RaiseParams) (domain.Task, error) {
	f.raised = append(f.raised, p)
	return domain.Task{TaskID: taskID, Attachments: p.Attachments}, nil
}
func (f *fakeRepo) Edit(context.Context, ports.EditParams) (domain.Task, error) { return f.task, nil }
func (f *fakeRepo) ChangeStatus(context.Context, ports.StatusParams) (domain.Task, error) {
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
	task, err := svc.Raise(ctx, ports.RaiseParams{
		TenantID: tenant, ActorID: raiser, AssigneeUserID: assignee, Title: " Call the vendor ", IdempotencyKey: "k",
		Refs: []domain.AttachmentRef{{ProofID: "p1", Kind: domain.AttachmentAudio, FileName: "note.m4a"}},
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
		Refs: []domain.AttachmentRef{{ProofID: "p1", Kind: domain.AttachmentFile}},
	}); !errors.Is(err, ports.ErrInvalidAttachment) {
		t.Fatalf("invalid attachment: %v", err)
	}
	if len(repo2.raised) != 0 {
		t.Fatal("nothing may be written when an attachment does not resolve")
	}
}

func TestGetTaskHidesATaskTheCallerIsNotPartyTo(t *testing.T) {
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
	if _, err := svc.GetTask(ctx, tenant, domain.Actor{UserID: assignee}, "not-a-uuid"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("bad id must read not-found, got %v", err)
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
