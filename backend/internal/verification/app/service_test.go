package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const testTenant = "00000000-0000-4000-8000-000000000001"

type fakeRepo struct {
	items           map[string]domain.Item
	byIdemKey       map[string]string
	createCalls     int
	seq             int
	verdictErr      error
	lastQueueParams ports.ListQueueParams
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

func (r *fakeRepo) GetItemCategories(_ context.Context, _ string, itemIDs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range itemIDs {
		if item, ok := r.items[id]; ok {
			out[id] = item.Category
		}
	}
	return out, nil
}

func (r *fakeRepo) GetItemProofRefs(_ context.Context, _ string, itemIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, id := range itemIDs {
		if item, ok := r.items[id]; ok {
			out[id] = item.MediaRefs
		}
	}
	return out, nil
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
	r.lastQueueParams = params
	out := make([]domain.Item, 0, len(r.items))
	for _, item := range r.items {
		if item.TenantID != params.TenantID {
			continue
		}
		if params.Status != "" && item.Status != params.Status {
			continue
		}
		if params.Category != "" && item.Category != params.Category {
			continue
		}
		// Mirrors the repository's own precedence: Categories applies only when the single Category
		// is empty, and an EMPTY Categories list means no category filter at all — which is exactly
		// why the service must never write an empty intersection back into it.
		if params.Category == "" && len(params.Categories) > 0 && !containsString(params.Categories, item.Category) {
			continue
		}
		if params.ParkID != "" && (item.ParkID == nil || *item.ParkID != params.ParkID) {
			continue
		}
		if params.ShedID != "" && (item.ShedID == nil || *item.ShedID != params.ShedID) {
			continue
		}
		if params.CapturedFrom != nil && item.CapturedAt.Before(*params.CapturedFrom) {
			continue
		}
		if params.CapturedBefore != nil && !item.CapturedAt.Before(*params.CapturedBefore) {
			continue
		}
		if params.SubmissionScopedOnly && item.Source.SubmissionID == nil {
			continue
		}
		if params.OpenOnly && item.ClosedAt != nil {
			continue
		}
		out = append(out, item)
	}
	if params.Limit > 0 && len(out) > params.Limit {
		out = out[:params.Limit]
	}
	return out, nil
}

func (r *fakeRepo) ListQueueFilterOptions(_ context.Context, params ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	var out domain.QueueFilterOptions
	seenParks := map[string]bool{}
	seenSheds := map[string]bool{}
	for _, item := range r.items {
		if item.TenantID != params.TenantID {
			continue
		}
		if params.Status != "" && item.Status != params.Status {
			continue
		}
		if params.Category != "" && item.Category != params.Category {
			continue
		}
		if params.CapturedFrom != nil && item.CapturedAt.Before(*params.CapturedFrom) {
			continue
		}
		if params.CapturedBefore != nil && !item.CapturedAt.Before(*params.CapturedBefore) {
			continue
		}
		if item.ParkID != nil && !seenParks[*item.ParkID] {
			seenParks[*item.ParkID] = true
			label := *item.ParkID
			if item.ParkLabel != nil {
				label = *item.ParkLabel
			}
			out.Parks = append(out.Parks, domain.LocationFilterOption{ID: *item.ParkID, Label: label})
		}
		if params.ParkID != "" && (item.ParkID == nil || *item.ParkID != params.ParkID) {
			continue
		}
		if item.ShedID != nil && !seenSheds[*item.ShedID] {
			seenSheds[*item.ShedID] = true
			label := *item.ShedID
			if item.ShedLabel != nil {
				label = *item.ShedLabel
			}
			out.Sheds = append(out.Sheds, domain.LocationFilterOption{ID: *item.ShedID, Label: label})
		}
	}
	if params.MissedBefore != nil {
		for _, item := range r.items {
			if item.TenantID == params.TenantID && item.Status == domain.StatusPending &&
				(params.Category == "" || item.Category == params.Category) && item.CapturedAt.Before(*params.MissedBefore) {
				out.HasMissed = true
				break
			}
		}
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

func (r *fakeRepo) ListReadyVaccinationBatchClosures(_ context.Context, _ ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	return nil, nil
}

func (r *fakeRepo) CloseVaccinationBatch(_ context.Context, in domain.CloseVaccinationBatchAction) ([]domain.Item, error) {
	return r.CloseSubmission(context.Background(), domain.CloseSubmissionAction{
		TenantID: in.TenantID, SubmissionID: in.BatchID, ActorID: in.ActorID, IdempotencyKey: in.IdempotencyKey,
	})
}

// MarkVerdictApplied is the apply-RECEIPT seam: the producing module reporting that
// it wrote the verdict's outcome onto its own record. It stamps only the receipt --
// never status, never verdict -- so the applier stays the single writer of the outcome.
func (r *fakeRepo) MarkVerdictApplied(_ context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string, appliedByModule string) (int, error) {
	applied := 0
	now := time.Now().UTC()
	for _, refID := range sourceRefIDs {
		for _, item := range r.items {
			if item.TenantID != tenantID || item.Source.Module != sourceModule || item.Source.RefType != sourceRefType || item.Source.RefID != refID {
				continue
			}
			if item.Status == domain.StatusPending || item.AppliedAt != nil {
				continue
			}
			stamped := now
			module := appliedByModule
			item.AppliedAt = &stamped
			item.AppliedByModule = &module
			applied++
		}
	}
	return applied, nil
}

func (r *fakeRepo) WithdrawItemsBySource(_ context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error) {
	withdrawn := 0
	for _, refID := range sourceRefIDs {
		for id, item := range r.items {
			if item.TenantID != tenantID || item.Source.Module != sourceModule || item.Source.RefType != sourceRefType || item.Source.RefID != refID {
				continue
			}
			if item.Status != domain.StatusPending {
				continue
			}
			item.Status = "withdrawn"
			item.RowVersion++
			r.items[id] = item
			withdrawn++
		}
	}
	return withdrawn, nil
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

// EnsureEvidenceAvailable: the default fake stands for "every proof object is still in storage".
func (fakeMedia) EnsureEvidenceAvailable(_ context.Context, _ string, proofIDs []string) error {
	if len(proofIDs) == 0 {
		return ports.ErrEvidenceMissing
	}
	return nil
}

var _ ports.EvidenceAvailabilityChecker = fakeMedia{}

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

func TestListQueueReturnsBackendLocationFilterOptions(t *testing.T) {
	svc, _ := newTestService()
	parkID := "00000000-0000-4000-8000-000000000101"
	parkLabel := "Godel Park"
	shedID := "00000000-0000-4000-8000-000000000102"
	shedLabel := "Godel 1"
	_, _ = svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs:      []string{"proof-1"},
		ParkID:         &parkID,
		ShedID:         &shedID,
		IdempotencyKey: "key-filter-options",
		CapturedAt:     time.Now(),
	})
	// The repository owns display labels; service/API must pass them through rather than making
	// Android infer location names from ids.
	for id, item := range svc.repo.(*fakeRepo).items {
		item.ParkLabel = &parkLabel
		item.ShedLabel = &shedLabel
		svc.repo.(*fakeRepo).items[id] = item
	}
	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant, Category: "vaccination_proof"})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.FilterOptions.Parks) != 1 || result.FilterOptions.Parks[0].ID != parkID || result.FilterOptions.Parks[0].Label != parkLabel {
		t.Fatalf("parks = %+v", result.FilterOptions.Parks)
	}
	if len(result.FilterOptions.Sheds) != 1 || result.FilterOptions.Sheds[0].ID != shedID || result.FilterOptions.Sheds[0].Label != shedLabel {
		t.Fatalf("sheds = %+v", result.FilterOptions.Sheds)
	}
}

func TestListQueueReturnsBackendPageOptionsForSelectedModule(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	for _, def := range []domain.CategoryDefinition{
		{
			Vertical: "counts", Module: "counts", Category: "birth_evidence",
			NavigationModule: "counts", NavigationModuleLabel: "Counts",
			PageKey: "birth", PageLabel: "Birth", PageOrder: 1,
		},
		{
			Vertical: "counts", Module: "counts", Category: "death_evidence",
			NavigationModule: "counts", NavigationModuleLabel: "Counts",
			PageKey: "death", PageLabel: "Death", PageOrder: 2,
		},
		{
			Vertical: "feed", Module: "feed", Category: "feed_packing",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 1,
		},
	} {
		if err := svc.RegisterCategory(def); err != nil {
			t.Fatalf("RegisterCategory(%q): %v", def.Category, err)
		}
	}

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant,
		Category: "death_evidence",
	})
	if err != nil {
		t.Fatalf("ListQueue() error = %v", err)
	}
	if result.FilterOptions.ModuleKey != "counts" || result.FilterOptions.ModuleLabel != "Counts" {
		t.Fatalf("module option = %+v", result.FilterOptions)
	}
	want := []domain.QueuePageOption{
		{Key: "birth", Label: "Birth", Category: "birth_evidence"},
		{Key: "death", Label: "Death", Category: "death_evidence"},
	}
	if !reflect.DeepEqual(result.FilterOptions.Pages, want) {
		t.Fatalf("pages = %+v want %+v", result.FilterOptions.Pages, want)
	}
	wantActionTypes := []domain.QueueActionTypeOption{
		{Key: "birth_evidence", Label: "Birth", Category: "birth_evidence", ModuleKey: "counts", ModuleLabel: "Counts"},
		{Key: "death_evidence", Label: "Death", Category: "death_evidence", ModuleKey: "counts", ModuleLabel: "Counts"},
		{Key: "feed_packing", Label: "Feed Packing", Category: "feed_packing", ModuleKey: "feed_direction", ModuleLabel: "Feed"},
	}
	if !reflect.DeepEqual(result.FilterOptions.ActionTypes, wantActionTypes) {
		t.Fatalf("action types = %+v want %+v", result.FilterOptions.ActionTypes, wantActionTypes)
	}
}

// registerModuleFixture registers a two-module, four-category registry: Feed spans three pages
// (the reason a module filter is not the same thing as a page filter) and Counts spans one.
func registerModuleFixture(t *testing.T, svc *Service) {
	t.Helper()
	for _, def := range []domain.CategoryDefinition{
		{
			Vertical: "feed", Module: "feed", Category: "feed_distribution",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_distribution", PageLabel: "Feed Distribution", PageOrder: 1,
		},
		{
			Vertical: "feed", Module: "feed", Category: "feed_packing",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2,
		},
		{
			Vertical: "feed", Module: "feed", Category: "feed_transport",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_transport", PageLabel: "Feed Transport", PageOrder: 3,
		},
		{
			Vertical: "counts", Module: "counts", Category: "birth_evidence",
			NavigationModule: "counts", NavigationModuleLabel: "Counts",
			PageKey: "birth", PageLabel: "Birth", PageOrder: 1,
		},
	} {
		if err := svc.RegisterCategory(def); err != nil {
			t.Fatalf("RegisterCategory(%q): %v", def.Category, err)
		}
	}
}

func TestListQueueModuleFilterExpandsToEveryCategoryOfThatModule(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	registerModuleFixture(t, svc)

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID:         testTenant,
		NavigationModule: "feed_direction",
	})
	if err != nil {
		t.Fatalf("ListQueue() error = %v", err)
	}
	// The predicate the repository actually received: all three Feed categories and nothing else.
	// Asserting the returned page alone would pass on an empty fixture no matter what was sent.
	want := []string{"feed_distribution", "feed_packing", "feed_transport"}
	if !reflect.DeepEqual(repo.lastQueueParams.Categories, want) {
		t.Fatalf("categories = %v want %v", repo.lastQueueParams.Categories, want)
	}
	if repo.lastQueueParams.Category != "" {
		t.Fatalf("single category must stay empty so the multi-category predicate applies, got %q", repo.lastQueueParams.Category)
	}
	// The module the caller picked also resolves its page chips, even though no single category
	// identifies it.
	if result.FilterOptions.ModuleKey != "feed_direction" || result.FilterOptions.ModuleLabel != "Feed" {
		t.Fatalf("selected module = %q/%q", result.FilterOptions.ModuleKey, result.FilterOptions.ModuleLabel)
	}
	if len(result.FilterOptions.Pages) != 3 {
		t.Fatalf("pages = %+v", result.FilterOptions.Pages)
	}
}

func TestListQueueModuleOptionsAreOnePerModuleNotPerCategory(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	registerModuleFixture(t, svc)

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant})
	if err != nil {
		t.Fatalf("ListQueue() error = %v", err)
	}
	want := []domain.QueueModuleOption{
		{Key: "counts", Label: "Counts"},
		{Key: "feed_direction", Label: "Feed"},
	}
	if !reflect.DeepEqual(result.FilterOptions.Modules, want) {
		t.Fatalf("modules = %+v want %+v", result.FilterOptions.Modules, want)
	}
}

// A verifier arrives with her duty categories already resolved into params.Categories. The module
// chip must INTERSECT with that, never replace it — and an intersection that comes out empty must
// be refused, because an empty category list reads as "every category" one layer down.
func TestListQueueModuleFilterNeverWidensAnAuthorizedCategorySet(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	registerModuleFixture(t, svc)

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID:         testTenant,
		Categories:       []string{"feed_packing", "birth_evidence"},
		NavigationModule: "feed_direction",
	})
	if err != nil {
		t.Fatalf("ListQueue() error = %v", err)
	}
	if want := []string{"feed_packing"}; !reflect.DeepEqual(repo.lastQueueParams.Categories, want) {
		t.Fatalf("categories = %v want %v (the module must not add feed_distribution/feed_transport)", repo.lastQueueParams.Categories, want)
	}
	_ = result

	_, err = svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID:         testTenant,
		Categories:       []string{"birth_evidence"},
		NavigationModule: "feed_direction",
	})
	if err == nil {
		t.Fatal("a module the caller holds no authorized category for must be refused, not served unfiltered")
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "module_scope_forbidden" {
		t.Fatalf("error = %v want module_scope_forbidden", err)
	}
}

func TestListQueueRejectsUnknownModuleAndConflictingCategory(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	registerModuleFixture(t, svc)

	// An unknown key is a bad request, never an empty queue that reads as "nothing to verify".
	_, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID:         testTenant,
		NavigationModule: "not_a_module",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_module" {
		t.Fatalf("unknown module error = %v want invalid_module", err)
	}

	_, err = svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID:         testTenant,
		Category:         "birth_evidence",
		NavigationModule: "feed_direction",
	})
	if !errors.As(err, &appErr) || appErr.Code != "module_category_conflict" {
		t.Fatalf("conflicting category error = %v want module_category_conflict", err)
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

func TestListQueueAcceptsPartitionGrainShedFilter(t *testing.T) {
	svc, _ := newTestService()
	shedID := "00000000-0000-4000-8000-000000000102"

	if _, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant,
		ShedID:   shedID + "#Part 3",
	}); err != nil {
		t.Fatalf("ListQueue() rejected partition-grain shed filter: %v", err)
	}
}

func TestListQueueRejectsMalformedPartitionGrainShedFilter(t *testing.T) {
	svc, _ := newTestService()
	shedID := "00000000-0000-4000-8000-000000000102"
	for _, filter := range []string{
		"#3",
		shedID + "#",
		shedID + "#1#2",
		"not-a-uuid#1",
	} {
		_, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant, ShedID: filter})
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "invalid_shed" {
			t.Fatalf("ListQueue(shed_id=%q) err = %v, want invalid_shed", filter, err)
		}
	}
}

func TestListQueueFiltersOneIndiaBusinessDateAndReturnsSecondaryTabs(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC) }
	create := func(key string, capturedAt time.Time) {
		_, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
			MediaRefs: []string{"proof-" + key}, IdempotencyKey: key, CapturedAt: capturedAt,
		})
		if err != nil {
			t.Fatalf("CreateItem(%s): %v", key, err)
		}
	}
	create("previous-ist-day", time.Date(2026, 7, 29, 18, 20, 0, 0, time.UTC))
	create("selected-ist-day", time.Date(2026, 7, 29, 19, 10, 0, 0, time.UTC))

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof", BusinessDate: "2026-07-30",
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Item.ItemID == "" {
		t.Fatalf("items = %+v, want one item captured on 2026-07-30 IST", result.Items)
	}
	wantStatuses := []domain.QueueStatusOption{
		// No "All": removed 2026-08-06 by maintainer decision. The three status chips are the ONLY
		// verification statuses, so an "All" chip can never surface a row the other three cannot.
		// The API still accepts `status=all`; it is simply not offered as phone chrome. Labels are
		// backend-owned per the verifier spec (verification-review page contract).
		{Key: "due", Label: "To verify", Status: domain.StatusPending},
		{Key: "approved", Label: "Accepted", Status: domain.StatusApproved},
		{Key: "rejected", Label: "Rejected", Status: domain.StatusRejected},
	}
	if !reflect.DeepEqual(result.FilterOptions.Statuses, wantStatuses) {
		t.Fatalf("statuses = %+v, want %+v", result.FilterOptions.Statuses, wantStatuses)
	}
	if result.FilterOptions.SelectedBusinessDate != "2026-07-30" || result.FilterOptions.BusinessTimezone != "Asia/Kolkata" {
		t.Fatalf("date options = %+v", result.FilterOptions)
	}
}

func TestListQueueMissedModeReturnsOlderPendingAndSignalsBell(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC) }
	for key, capturedAt := range map[string]time.Time{
		"missed": time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC),
		"today":  time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC),
	} {
		_, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
			MediaRefs: []string{"proof-" + key}, IdempotencyKey: key, CapturedAt: capturedAt,
		})
		if err != nil {
			t.Fatalf("CreateItem(%s): %v", key, err)
		}
	}

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof", MissedOnly: true,
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.Items) != 1 || !result.FilterOptions.MissedOnly || !result.FilterOptions.HasMissed {
		t.Fatalf("missed result = %+v options=%+v", result.Items, result.FilterOptions)
	}
}

func TestListQueueRejectsFutureOrConflictingDateScope(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC) }
	for _, params := range []ports.ListQueueParams{
		{TenantID: testTenant, BusinessDate: "2026-07-31"},
		{TenantID: testTenant, BusinessDate: "2026-07-29", MissedOnly: true},
		{TenantID: testTenant, Status: domain.StatusApproved, MissedOnly: true},
	} {
		if _, err := svc.ListQueue(context.Background(), params); err == nil {
			t.Fatalf("ListQueue(%+v) error = nil", params)
		}
	}
}

// The Actions board's capture-date picker sends a span when the verifier asks for one. The range
// is INCLUSIVE on both ends in Asia/Kolkata, which is the whole reason it cannot be expressed as
// two instants: 2026-07-29T18:20Z is already 2026-07-30 IST, so a naive UTC bound would drop it
// from a range ending on the 30th.
func TestListQueueFiltersAnInclusiveIndiaBusinessDateRange(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC) }
	create := func(key string, capturedAt time.Time) {
		_, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
			MediaRefs: []string{"proof-" + key}, IdempotencyKey: key, CapturedAt: capturedAt,
		})
		if err != nil {
			t.Fatalf("CreateItem(%s): %v", key, err)
		}
	}
	// IST business dates: 28th (below the range), 29th (lower edge), 30th (upper edge), 31st (above).
	create("before-range", time.Date(2026, 7, 27, 20, 0, 0, 0, time.UTC))
	create("lower-edge", time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC))
	create("upper-edge", time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC))
	create("after-range", time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC))

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof",
		BusinessDateFrom: "2026-07-29", BusinessDateTo: "2026-07-30",
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want the 2 items captured on 2026-07-29 and 2026-07-30 IST: %+v", len(result.Items), result.Items)
	}
}

// A single-day range and the single-day param must select the SAME set — the admin-web picker
// collapses from == to into business_date, so a disagreement here would make the same calendar
// click return different rows depending on which encoding the page chose.
func TestListQueueSingleDayRangeMatchesBusinessDate(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC) }
	for key, capturedAt := range map[string]time.Time{
		"in":  time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC), // 2026-07-30 IST
		"out": time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC), // 2026-07-29 IST
	} {
		if _, err := svc.CreateItem(context.Background(), domain.CreateItem{
			TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
			MediaRefs: []string{"proof-" + key}, IdempotencyKey: key, CapturedAt: capturedAt,
		}); err != nil {
			t.Fatalf("CreateItem(%s): %v", key, err)
		}
	}
	single, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant, BusinessDate: "2026-07-30"})
	if err != nil {
		t.Fatalf("ListQueue(business_date): %v", err)
	}
	asRange, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, BusinessDateFrom: "2026-07-30", BusinessDateTo: "2026-07-30",
	})
	if err != nil {
		t.Fatalf("ListQueue(range): %v", err)
	}
	if len(single.Items) != 1 || len(asRange.Items) != len(single.Items) {
		t.Fatalf("single=%d range=%d, want both to select exactly the one item captured on 2026-07-30 IST", len(single.Items), len(asRange.Items))
	}
}

func TestListQueueRejectsMalformedOrConflictingDateRange(t *testing.T) {
	svc, _ := newTestService()
	svc.now = func() time.Time { return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC) }
	for name, tc := range map[string]struct {
		params ports.ListQueueParams
		want   string
	}{
		// A half-open range would have to invent the missing end, and the two plausible inventions
		// (today, or the beginning of time) mean opposite things to a verifier.
		"missing upper end": {ports.ListQueueParams{TenantID: testTenant, BusinessDateFrom: "2026-07-29"}, "invalid_business_date_range"},
		"missing lower end": {ports.ListQueueParams{TenantID: testTenant, BusinessDateTo: "2026-07-29"}, "invalid_business_date_range"},
		"inverted":          {ports.ListQueueParams{TenantID: testTenant, BusinessDateFrom: "2026-07-30", BusinessDateTo: "2026-07-28"}, "invalid_business_date_range"},
		"future upper end":  {ports.ListQueueParams{TenantID: testTenant, BusinessDateFrom: "2026-07-29", BusinessDateTo: "2026-07-31"}, "future_business_date"},
		"not a date":        {ports.ListQueueParams{TenantID: testTenant, BusinessDateFrom: "yesterday", BusinessDateTo: "2026-07-29"}, "invalid_business_date"},
		// The three date scopes are mutually exclusive; combining them asks for a contradiction.
		"with business_date": {ports.ListQueueParams{TenantID: testTenant, BusinessDate: "2026-07-29", BusinessDateFrom: "2026-07-28", BusinessDateTo: "2026-07-29"}, "invalid_date_scope"},
		"with missed":        {ports.ListQueueParams{TenantID: testTenant, MissedOnly: true, BusinessDateFrom: "2026-07-28", BusinessDateTo: "2026-07-29"}, "invalid_date_scope"},
	} {
		_, err := svc.ListQueue(context.Background(), tc.params)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != tc.want {
			t.Fatalf("%s: ListQueue err = %v, want %s", name, err, tc.want)
		}
	}
}

func TestListQueueCanIncludeAllStatusesForLeadershipReview(t *testing.T) {
	svc, repo := newTestService()
	submissionID := "00000000-0000-4000-8000-000000000031"
	pending, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", SubmissionID: &submissionID, RefType: "vaccination_goat", RefID: testTenant},
		MediaRefs: []string{"proof-pending"}, IdempotencyKey: "pending",
	})
	approved, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", SubmissionID: &submissionID, RefType: "vaccination_goat", RefID: testTenant},
		MediaRefs: []string{"proof-approved"}, IdempotencyKey: "approved",
	})
	_, _ = svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: approved.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: approved.Item.RowVersion,
	})
	rejected, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", SubmissionID: &submissionID, RefType: "vaccination_goat", RefID: testTenant},
		MediaRefs: []string{"proof-rejected"}, IdempotencyKey: "rejected",
	})
	_, _ = svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: rejected.Item.ItemID, Decision: domain.DecisionRejected, Reason: "too dark",
		VerifierID: testTenant, RowVersion: rejected.Item.RowVersion,
	})

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof", IncludeAllStatuses: true, SubmissionScopedOnly: true, OpenOnly: true,
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("leadership items=%d, want 3; pending=%s repo=%d", len(result.Items), pending.Item.ItemID, len(repo.items))
	}
}

// TestLeadershipReviewIncludesClosedItemsWhileActionQueueExcludesThem pins the exact
// defect the CEO reported: "where are the videos that [are] pending accepted rejected" —
// the Videos surface silently dropped half his approved vaccination proofs because they
// had already been closed. Reproduces the real shape from live evidence (2 approved+
// closed, 2 approved+open would also apply, 1 rejected+open) with the minimal case that
// isolates the OpenOnly bug: a submission with one approved item that gets closed.
//
// The leadership REVIEW read (OpenOnly=false, the shape GET /verification/queue now
// uses for every principal, leadership included — see the nav-registry fix routing
// vaccination's "videos" nav item through verifyQueueHref instead of "/verify/action")
// must still return the closed item: leadership's Videos tab is an audit trail, and a
// closed item is finished work, not vanished work.
//
// The verifier's ACTION queue (OpenOnly=true, GET /verification/action-queue) must keep
// excluding it: that queue answers "what needs my action right now", and a closed item
// needs no more action. Do NOT fix the leadership gap by flipping OpenOnly for the action
// queue (see the reverted "OpenOnly: !actionQueue" change) — that would flood the
// verifier's actionable list with settled work instead of routing leadership to the
// correct read.
func TestLeadershipReviewIncludesClosedItemsWhileActionQueueExcludesThem(t *testing.T) {
	svc, _ := newTestService()
	submissionID := "00000000-0000-4000-8000-000000000041"
	approved, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:       "vaccination",
			SubmissionID: &submissionID,
			RefType:      "vaccination_goat",
			RefID:        testTenant,
		},
		MediaRefs: []string{"proof-approved-closed"}, IdempotencyKey: "approved-closed",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if _, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: approved.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: approved.Item.RowVersion,
	}); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	closed, err := svc.CloseSubmission(context.Background(), domain.CloseSubmissionAction{
		TenantID: testTenant, SubmissionID: submissionID, ActorID: testTenant,
	})
	if err != nil {
		t.Fatalf("CloseSubmission: %v", err)
	}
	if len(closed) != 1 || closed[0].ClosedAt == nil {
		t.Fatalf("closed items=%+v, want exactly 1 with ClosedAt set", closed)
	}

	// Leadership REVIEW read: OpenOnly=false, all statuses. The closed approved item MUST
	// still be there.
	review, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof", IncludeAllStatuses: true, OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListQueue (review): %v", err)
	}
	foundClosed := false
	for _, row := range review.Items {
		if row.Item.ItemID == approved.Item.ItemID {
			foundClosed = true
			if row.Item.ClosedAt == nil {
				t.Fatalf("review item %s lost its ClosedAt", row.Item.ItemID)
			}
		}
	}
	if !foundClosed {
		t.Fatalf("leadership review queue dropped the closed approved item; items=%+v", review.Items)
	}

	// Verifier ACTION queue: OpenOnly=true. The closed item must NOT reappear here — it
	// needs no action.
	actionQueue, err := svc.ListQueue(context.Background(), ports.ListQueueParams{
		TenantID: testTenant, Category: "vaccination_proof", IncludeAllStatuses: true,
		SubmissionScopedOnly: true, OpenOnly: true,
	})
	if err != nil {
		t.Fatalf("ListQueue (action queue): %v", err)
	}
	for _, row := range actionQueue.Items {
		if row.Item.ItemID == approved.Item.ItemID {
			t.Fatalf("verifier action queue must exclude the closed item, got it: %+v", row.Item)
		}
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

// labellingMedia mimics the real resolver's richer path: it returns a workflow-task title for one
// proof and nothing for the other, which is exactly the mixed case the fallback has to respect.
type labellingMedia struct{ labelled map[string]string }

func (m labellingMedia) ResolveMedia(_ context.Context, _ string, proofIDs []string) ([]domain.MediaItem, error) {
	out := make([]domain.MediaItem, 0, len(proofIDs))
	for _, id := range proofIDs {
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: "https://signed.example/" + id, Label: m.labelled[id]})
	}
	return out, nil
}

// Every proof reaching a renderer carries a header, and a richer workflow-task title is never
// clobbered by the registry fallback.
func TestListQueueLabelsEveryProof(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, labellingMedia{labelled: map[string]string{"proof-b": "Iodine dipping of umbilical cord"}})
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "feed", Module: "feed", Category: "feed_distribution",
		ExpectedMedia: []string{"video", "photo_or_video"},
		MediaLabels:   []string{"Feed distribution video", "Water distribution proof"},
	}); err != nil {
		t.Fatalf("RegisterCategory: %v", err)
	}
	if _, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "feed", Module: "feed", Category: "feed_distribution",
		Source:    domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: testTenant},
		MediaRefs: []string{"proof-a", "proof-b"}, IdempotencyKey: "label-key-1", CapturedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	media := result.Items[0].Media
	if len(media) != 2 {
		t.Fatalf("media = %d, want 2", len(media))
	}
	// Blank label filled from the registry's declared copy.
	if media[0].Label != "Feed distribution video" {
		t.Fatalf("media[0].Label = %q, want the declared registry label", media[0].Label)
	}
	// Workflow-task truth is more specific than the registry's positional copy and must survive.
	if media[1].Label != "Iodine dipping of umbilical cord" {
		t.Fatalf("media[1].Label = %q, want the workflow task title preserved", media[1].Label)
	}
	for i, m := range media {
		if strings.TrimSpace(m.Label) == "" {
			t.Fatalf("media[%d] reached the renderer with no header", i)
		}
	}
}

// A category registered without media labels still yields a header for every proof.
func TestListQueueLabelsProofsForCategoryWithoutDeclaredLabels(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fakeMedia{})
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "counts", Module: "counts", Category: "milk_preparation",
		ExpectedMedia: []string{"video", "video", "video"},
	}); err != nil {
		t.Fatalf("RegisterCategory: %v", err)
	}
	if _, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "counts", Module: "counts", Category: "milk_preparation",
		Source:    domain.SourceRef{Module: "counts", RefType: "milk_preparation", RefID: testTenant},
		MediaRefs: []string{"p1", "p2", "p3"}, IdempotencyKey: "label-key-2", CapturedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	result, err := svc.ListQueue(context.Background(), ports.ListQueueParams{TenantID: testTenant})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	for i, want := range []string{"Video 1", "Video 2", "Video 3"} {
		if got := result.Items[0].Media[i].Label; got != want {
			t.Fatalf("media[%d].Label = %q, want %q", i, got, want)
		}
	}
}

type failingMedia struct{}

func (failingMedia) ResolveMedia(_ context.Context, _ string, _ []string) ([]domain.MediaItem, error) {
	return nil, errors.New("proof resolver unavailable")
}

// evidence_available (domain: EvidenceLinkResolved) is a LINK-RESOLUTION claim by design. The queue
// read must NOT stat stored objects (N+1 on a hot operator read); a link that resolves but whose
// bytes are gone is still reported true here and is caught terminally by the download route
// (410 proof_object_missing, retryable=false).
func TestEvidenceLinkResolvedIsLinkResolutionNotByteRetrievability(t *testing.T) {
	item := domain.Item{ItemID: "item-1", TenantID: testTenant, MediaRefs: []string{"proof-a", "proof-b"}}

	// All refs resolve to signed links -> true. fakeMedia never touches storage bytes, which is
	// exactly the production behaviour being documented.
	svc, _ := newTestService()
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{item})
	if len(rows) != 1 || !rows[0].EvidenceLinkResolved {
		t.Fatalf("EvidenceLinkResolved = %v, want true when every media_ref resolved a link", rows[0].EvidenceLinkResolved)
	}
	if len(rows[0].Media) != 2 {
		t.Fatalf("media len = %d, want 2", len(rows[0].Media))
	}

	// Resolver failure fails closed -> false, and no partial media list leaks.
	failing := NewService(newFakeRepo(), failingMedia{})
	rows = failing.resolveMedia(context.Background(), testTenant, []domain.Item{item})
	if rows[0].EvidenceLinkResolved {
		t.Fatal("EvidenceLinkResolved = true when the proof resolver failed, want false")
	}
	if len(rows[0].Media) != 0 {
		t.Fatalf("media len = %d on resolver failure, want 0", len(rows[0].Media))
	}

	// No media refs at all -> false (nothing to show the verifier).
	rows = svc.resolveMedia(context.Background(), testTenant, []domain.Item{{ItemID: "item-2", TenantID: testTenant}})
	if rows[0].EvidenceLinkResolved {
		t.Fatal("EvidenceLinkResolved = true for an item with no media_refs, want false")
	}
}

// One unresolvable media ref per item should NOT blank the video for other healthy items on the page.
// Per-item resolution: an item whose own refs all resolve keeps its media and evidence_available=true;
// an item with any unresolvable ref of its OWN gets empty media + evidence_available=false.
func TestPerItemMediaResolution(t *testing.T) {
	// Two items: one with unresolvable ref, one healthy. The healthy item must retain its media.
	item1 := domain.Item{ItemID: "item-1", TenantID: testTenant, MediaRefs: []string{"proof-missing"}}
	item2 := domain.Item{ItemID: "item-2", TenantID: testTenant, MediaRefs: []string{"proof-valid"}}

	// Resolver returns per-ID failures as empty MediaItems (DownloadURL="")
	failOnID := map[string]bool{"proof-missing": true}
	partialResolver := &partialMediaResolver{failOnID: failOnID}

	svc := NewService(newFakeRepo(), partialResolver)
	rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{item1, item2})

	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}

	// Item 1: unresolvable ref -> empty media, evidence_available=false
	if rows[0].EvidenceLinkResolved {
		t.Errorf("item1.EvidenceLinkResolved = true, want false (ref failed to resolve)")
	}
	if len(rows[0].Media) != 0 {
		t.Errorf("item1 media len = %d, want 0", len(rows[0].Media))
	}

	// Item 2: all refs resolved -> media present, evidence_available=true
	if !rows[1].EvidenceLinkResolved {
		t.Errorf("item2.EvidenceLinkResolved = false, want true (all refs resolved)")
	}
	if len(rows[1].Media) != 1 {
		t.Errorf("item2 media len = %d, want 1", len(rows[1].Media))
	}
	if rows[1].Media[0].DownloadURL == "" {
		t.Error("item2 media has empty DownloadURL")
	}
}

// Helper: partial resolver for testing (returns empty MediaItems for failed IDs, valid ones for others)
type partialMediaResolver struct {
	failOnID map[string]bool
}

func (p *partialMediaResolver) ResolveMedia(_ context.Context, _ string, proofIDs []string) ([]domain.MediaItem, error) {
	out := make([]domain.MediaItem, len(proofIDs))
	for i, id := range proofIDs {
		out[i].ProofID = id
		if p.failOnID[id] {
			continue // Leave DownloadURL empty for failed IDs
		}
		out[i].DownloadURL = "https://example.com/download/" + id
	}
	return out, nil
}
