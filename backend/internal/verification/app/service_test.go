package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const testTenant = "00000000-0000-4000-8000-000000000001"

type fakeRepo struct {
	items       map[string]domain.Item
	byIdemKey   map[string]string
	createCalls int
	seq         int
	verdictErr  error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{items: map[string]domain.Item{}, byIdemKey: map[string]string{}}
}

func (r *fakeRepo) CreateItem(_ context.Context, in domain.CreateItem) (domain.CreateItemResult, error) {
	r.createCalls++
	idemKey := in.TenantID + "|" + in.IdempotencyKey
	if existingID, ok := r.byIdemKey[idemKey]; ok {
		return domain.CreateItemResult{Item: r.items[existingID], Created: false}, nil
	}
	r.seq++
	id := fmt.Sprintf("00000000-0000-4000-9000-%012d", r.seq)
	r.byIdemKey[idemKey] = id
	item := domain.Item{
		ItemID:       id,
		TenantID:     in.TenantID,
		Vertical:     in.Vertical,
		Module:       in.Module,
		Category:     in.Category,
		SubjectLabel: in.SubjectLabel,
		Source:       in.Source,
		MediaRefs:    in.MediaRefs,
		Status:       domain.StatusPending,
		OperatorID:   in.OperatorID,
		ShedID:       in.ShedID,
		ParkID:       in.ParkID,
		CapturedAt:   in.CapturedAt,
		RowVersion:   1,
	}
	r.items[id] = item
	return domain.CreateItemResult{Item: item, Created: true}, nil
}

func (r *fakeRepo) GetItem(_ context.Context, _ string, itemID string) (domain.Item, error) {
	item, ok := r.items[itemID]
	if !ok {
		return domain.Item{}, ports.ErrNotFound
	}
	return item, nil
}

func (r *fakeRepo) GetSubmissionItems(_ context.Context, tenantID, submissionID string) ([]domain.Item, error) {
	items := make([]domain.Item, 0)
	for _, item := range r.items {
		if item.TenantID == tenantID && item.Source.SubmissionID != nil && *item.Source.SubmissionID == submissionID {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, ports.ErrNotFound
	}
	return items, nil
}

func (r *fakeRepo) ListQueue(_ context.Context, params ports.ListQueueParams) ([]domain.Item, error) {
	out := make([]domain.Item, 0, len(r.items))
	for _, item := range r.items {
		if item.TenantID != params.TenantID || item.Status != params.Status {
			continue
		}
		if params.Category != "" && item.Category != params.Category {
			continue
		}
		out = append(out, item)
	}
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, nil
}

func (r *fakeRepo) RecordVerdict(_ context.Context, in domain.Verdict) (domain.Item, error) {
	if r.verdictErr != nil {
		return domain.Item{}, r.verdictErr
	}
	item, ok := r.items[in.ItemID]
	if !ok {
		return domain.Item{}, ports.ErrNotFound
	}
	if item.RowVersion != in.RowVersion {
		return domain.Item{}, ports.ErrConflict
	}
	switch in.Decision {
	case domain.DecisionApproved:
		item.Status = domain.StatusApproved
	case domain.DecisionRejected:
		item.Status = domain.StatusRejected
		reason := in.Reason
		item.VerdictReason = &reason
	}
	verifier := in.VerifierID
	item.VerifiedBy = &verifier
	item.RowVersion++
	r.items[in.ItemID] = item
	return item, nil
}

func (r *fakeRepo) CloseItem(_ context.Context, in domain.CloseAction) (domain.Item, error) {
	item, ok := r.items[in.ItemID]
	if !ok {
		return domain.Item{}, ports.ErrNotFound
	}
	if item.RowVersion != in.RowVersion || item.Status != domain.StatusApproved || item.ClosedAt != nil {
		return domain.Item{}, ports.ErrConflict
	}
	now := time.Now().UTC()
	actor := in.ActorID
	item.ClosedAt = &now
	item.ClosedBy = &actor
	item.RowVersion++
	r.items[in.ItemID] = item
	return item, nil
}

func (r *fakeRepo) CloseSubmission(_ context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error) {
	items, err := r.GetSubmissionItems(context.Background(), in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Status != domain.StatusApproved {
			return nil, ports.ErrConflict
		}
	}
	now := time.Now().UTC()
	for i, item := range items {
		if item.ClosedAt == nil {
			actor := in.ActorID
			item.ClosedAt = &now
			item.ClosedBy = &actor
			item.RowVersion++
			r.items[item.ItemID] = item
			items[i] = item
		}
	}
	return items, nil
}

var _ ports.Repository = (*fakeRepo)(nil)

type fakeMedia struct{}

func (fakeMedia) ResolveMedia(_ context.Context, _ string, proofIDs []string) ([]domain.MediaItem, error) {
	out := make([]domain.MediaItem, 0, len(proofIDs))
	for _, id := range proofIDs {
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: "https://signed.example/" + id})
	}
	return out, nil
}

func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeMedia{})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof"})
	return svc, repo
}

func TestCreateItemRejectsUnknownCategory(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID:       testTenant,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "not_registered",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs:      []string{"proof-1"},
		IdempotencyKey: "key-1",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "unknown_category" {
		t.Fatalf("err = %v, want unknown_category", err)
	}
}

func TestCreateItemRequiresMedia(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID:       testTenant,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		IdempotencyKey: "key-1",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_media" {
		t.Fatalf("err = %v, want invalid_media", err)
	}
}

func TestCreateItemSucceeds(t *testing.T) {
	svc, repo := newTestService()
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID:       testTenant,
		Vertical:       "preventive_care",
		Module:         "vaccination",
		Category:       "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs:      []string{"proof-1", "proof-2"},
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if !result.Created || result.Item.Status != domain.StatusPending {
		t.Fatalf("result = %+v", result)
	}
	if repo.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", repo.createCalls)
	}
}

func TestCreateItemIsIdempotentOnReplay(t *testing.T) {
	svc, repo := newTestService()
	in := domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs:      []string{"proof-1"},
		IdempotencyKey: "vaccination:submission:sub-1",
	}
	first, err := svc.CreateItem(context.Background(), in)
	if err != nil {
		t.Fatalf("first CreateItem: %v", err)
	}
	second, err := svc.CreateItem(context.Background(), in)
	if err != nil {
		t.Fatalf("replay CreateItem: %v", err)
	}
	if second.Created {
		t.Fatal("replay with the same idempotency key must not mint a new item")
	}
	if second.Item.ItemID != first.Item.ItemID {
		t.Fatalf("replay returned a different item: %s vs %s", second.Item.ItemID, first.Item.ItemID)
	}
	if repo.createCalls != 2 {
		t.Fatalf("createCalls = %d, want 2 (both attempts reach the repo)", repo.createCalls)
	}
}

func TestRecordVerdictRequiresReasonToReject(t *testing.T) {
	svc, repo := newTestService()
	result, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: result.Item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: testTenant, RowVersion: 1,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 422 {
		t.Fatalf("err = %v, want 422 reason_required", err)
	}
	if appErr.Code != "reason_required" {
		t.Fatalf("code = %s, want reason_required", appErr.Code)
	}
	_ = repo
}

func TestRecordVerdictApprovedNoReasonRequired(t *testing.T) {
	svc, _ := newTestService()
	result, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	item, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: result.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if item.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", item.Status)
	}
}

func TestRecordVerdictRejectsInvalidDecision(t *testing.T) {
	svc, _ := newTestService()
	result, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: result.Item.ItemID, Decision: "maybe",
		VerifierID: testTenant, RowVersion: 1, Reason: "because",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_decision" {
		t.Fatalf("err = %v, want invalid_decision", err)
	}
}

func TestRecordVerdictConflictOnStaleRowVersion(t *testing.T) {
	svc, _ := newTestService()
	result, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: result.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 99,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
		t.Fatalf("err = %v, want 409 conflict", err)
	}
}

func TestListQueueDefaultsToPendingAndResolvesMedia(t *testing.T) {
	svc, _ := newTestService()
	_, _ = svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1", CapturedAt: time.Now(),
	})
	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	if len(result.Items[0].Media) != 1 || result.Items[0].Media[0].DownloadURL == "" {
		t.Fatalf("media not resolved: %+v", result.Items[0].Media)
	}
}

func TestListQueueRejectsInvalidStatus(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant, Status: "bogus"})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_status" {
		t.Fatalf("err = %v, want invalid_status", err)
	}
}

func TestCloseSubmissionRequiresEveryGoatApprovedAndClosesDriveTogether(t *testing.T) {
	svc, _ := newTestService()
	submissionID := "00000000-0000-4000-8000-000000000021"
	create := func(key string) domain.CreateItemResult {
		result, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source: domain.SourceRef{
				Module:       "vaccination",
				SubmissionID: &submissionID,
				RefType:      "vaccination_goat",
				RefID:        testTenant,
			},
			MediaRefs: []string{"proof-" + key}, IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("CreateItem: %v", err)
		}
		return result
	}
	first := create("first")
	second := create("second")
	_, _ = svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: first.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	_, err := svc.CloseSubmission(context.Background(), domain.CloseSubmissionAction{
		TenantID: testTenant, SubmissionID: submissionID, ActorID: testTenant,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
		t.Fatalf("partial approval close err=%v, want 409", err)
	}

	_, _ = svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: second.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	items, err := svc.CloseSubmission(context.Background(), domain.CloseSubmissionAction{
		TenantID: testTenant, SubmissionID: submissionID, ActorID: testTenant,
	})
	if err != nil {
		t.Fatalf("CloseSubmission: %v", err)
	}
	if len(items) != 2 || items[0].ClosedAt == nil || items[1].ClosedAt == nil {
		t.Fatalf("closed items=%+v", items)
	}
}
