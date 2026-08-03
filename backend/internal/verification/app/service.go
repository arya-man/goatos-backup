// Package app implements the Verification module's use-cases: producers enqueue items, a Verifier
// lists their category-filtered queue and records an approve/reject verdict.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const (
	defaultQueueLimit = 20
	maxQueueLimit     = 100
)

type Service struct {
	repo     ports.Repository
	media    ports.MediaResolver
	registry *domain.Registry
	now      func() time.Time
}

func NewService(repo ports.Repository, media ports.MediaResolver) *Service {
	return &Service{repo: repo, media: media, registry: domain.NewRegistry(), now: time.Now}
}

// RegisterCategory adds one plug-and-play category entry (composition-time wiring; see
// verification-module-design.md §2.3). CreateItem rejects any category that is not registered.
func (s *Service) RegisterCategory(def domain.CategoryDefinition) error {
	return s.registry.Register(def)
}

// Categories lists every registered category (admin-web/mobile filter chips read this).
func (s *Service) Categories() []domain.CategoryDefinition {
	return s.registry.List()
}

// CreateItem is the producer-facing API: any module (vaccination first; feed/diagnosis/death/
// breeding later) calls this to enqueue one verification item. Verification never reaches into a
// producer's tables — everything it needs travels in CreateItem.
func (s *Service) CreateItem(ctx context.Context, in domain.CreateItem) (domain.CreateItemResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Vertical = strings.TrimSpace(in.Vertical)
	in.Module = strings.TrimSpace(in.Module)
	in.Category = strings.TrimSpace(in.Category)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.Source.Module = strings.TrimSpace(in.Source.Module)
	in.Source.RefType = strings.TrimSpace(in.Source.RefType)
	in.Source.RefID = strings.TrimSpace(in.Source.RefID)
	if !uuidutil.IsUUIDString(in.TenantID) {
		return domain.CreateItemResult{}, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	if in.Vertical == "" || in.Module == "" || in.Category == "" {
		return domain.CreateItemResult{}, BadRequest("invalid_item", "vertical, module, and category are required")
	}
	if _, ok := s.registry.Get(in.Category); !ok {
		return domain.CreateItemResult{}, BadRequest("unknown_category", fmt.Sprintf("category %q is not registered", in.Category))
	}
	if in.Source.Module == "" || in.Source.RefType == "" || in.Source.RefID == "" {
		return domain.CreateItemResult{}, BadRequest("invalid_source_ref", "source module, ref_type, and ref_id are required")
	}
	if len(in.MediaRefs) == 0 {
		return domain.CreateItemResult{}, BadRequest("invalid_media", "at least one media reference is required")
	}
	if in.IdempotencyKey == "" {
		return domain.CreateItemResult{}, BadRequest("invalid_idempotency_key", "idempotency_key is required")
	}
	if in.CapturedAt.IsZero() {
		in.CapturedAt = s.now().UTC()
	}
	result, err := s.repo.CreateItem(ctx, in)
	if err != nil {
		return domain.CreateItemResult{}, mapRepoErr(err)
	}
	return result, nil
}

// QueueResult is one page of the verifier queue.
type QueueResult struct {
	Items         []domain.QueueRow
	FilterOptions domain.QueueFilterOptions
	NextCursor    *string
}

// ListQueue returns a keyset page (~20 default, ~100 max) of items, category/vertical/module/status
// filtered, oldest-captured-first. Media for the whole page is resolved in ONE batched call.
func (s *Service) ListQueue(ctx context.Context, params ports.ListQueueParams) (QueueResult, error) {
	params.TenantID = strings.TrimSpace(params.TenantID)
	if !uuidutil.IsUUIDString(params.TenantID) {
		return QueueResult{}, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	params.Category = strings.TrimSpace(params.Category)
	params.Vertical = strings.TrimSpace(params.Vertical)
	params.Module = strings.TrimSpace(params.Module)
	params.Status = strings.TrimSpace(params.Status)
	params.ParkID = strings.TrimSpace(params.ParkID)
	params.ShedID = strings.TrimSpace(params.ShedID)
	if params.ParkID != "" && !uuidutil.IsUUIDString(params.ParkID) {
		return QueueResult{}, BadRequest("invalid_park", "park_id must be a UUID")
	}
	if params.ShedID != "" && !uuidutil.IsUUIDString(params.ShedID) {
		return QueueResult{}, BadRequest("invalid_shed", "shed_id must be a UUID")
	}
	if params.Status == "" && !params.IncludeAllStatuses {
		params.Status = domain.StatusPending
	}
	if params.Status != "" && !oneOf(params.Status, domain.StatusPending, domain.StatusApproved, domain.StatusRejected) {
		return QueueResult{}, BadRequest("invalid_status", "status must be pending, approved, or rejected")
	}
	params.Limit = boundedLimit(params.Limit, maxQueueLimit)
	requested := params.Limit
	params.Limit++
	items, err := s.repo.ListQueue(ctx, params)
	if err != nil {
		return QueueResult{}, mapRepoErr(err)
	}
	var next *string
	if len(items) > requested {
		items = items[:requested]
		last := items[len(items)-1]
		encoded, encErr := domain.EncodeCursor(domain.Cursor{CapturedAt: last.CapturedAt, ItemID: last.ItemID})
		if encErr != nil {
			return QueueResult{}, fmt.Errorf("verification: invalid pagination cursor: %w", encErr)
		}
		next = &encoded
	}
	options, err := s.repo.ListQueueFilterOptions(ctx, params)
	if err != nil {
		return QueueResult{}, mapRepoErr(err)
	}
	return QueueResult{Items: s.resolveMedia(ctx, params.TenantID, items), FilterOptions: options, NextCursor: next}, nil
}

func (s *Service) ListReadyVaccinationBatchClosures(ctx context.Context, params ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	params.TenantID = strings.TrimSpace(params.TenantID)
	if !uuidutil.IsUUIDString(params.TenantID) {
		return nil, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	params.Category = strings.TrimSpace(params.Category)
	params.Vertical = strings.TrimSpace(params.Vertical)
	params.Module = strings.TrimSpace(params.Module)
	closures, err := s.repo.ListReadyVaccinationBatchClosures(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return closures, nil
}

// resolveMedia batch-resolves every distinct proof id referenced on the page in ONE call to the proof
// storage signed-URL port (never a per-row lookup — bounded by page size x media-per-item).
//
// It deliberately does NOT verify that each stored object is retrievable. Doing so would cost one
// stat/HEAD per proof per row: on GCS (the production provider) a signed HEAD is ~20-50ms, so a
// 20-item page with ~3 proofs each is ~60 sequential round trips (~1.2-3s) — far past the sub-500ms
// operator hot-read budget, and an N+1 on a queue read. The honest contract is therefore
// EvidenceLinkResolved ("a link was issued for every media_ref"), and terminal unavailability is
// reported by the download route as 410 proof_object_missing / retryable=false for the client to
// render as "evidence unavailable".
func (s *Service) resolveMedia(ctx context.Context, tenantID string, items []domain.Item) []domain.QueueRow {
	rows := make([]domain.QueueRow, len(items))
	allProofIDs := make([]string, 0, len(items)*3)
	seen := map[string]struct{}{}
	for _, it := range items {
		for _, id := range it.MediaRefs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			allProofIDs = append(allProofIDs, id)
		}
	}
	mediaByID := map[string]domain.MediaItem{}
	resolutionOK := len(allProofIDs) > 0 && s.media != nil
	if resolutionOK {
		resolved, err := s.media.ResolveMedia(ctx, tenantID, allProofIDs)
		if err == nil && len(resolved) == len(allProofIDs) {
			for _, m := range resolved {
				mediaByID[m.ProofID] = m
			}
		} else {
			resolutionOK = false
		}
	}
	for i, it := range items {
		media := make([]domain.MediaItem, 0, len(it.MediaRefs))
		for _, id := range it.MediaRefs {
			if m, ok := mediaByID[id]; ok {
				media = append(media, m)
			}
		}
		rows[i] = domain.QueueRow{Item: it, Media: media, EvidenceLinkResolved: resolutionOK && len(media) == len(it.MediaRefs)}
	}
	return rows
}

// RecordVerdict applies the verifier's approve/reject decision with optimistic concurrency. Reject
// REQUIRES a non-empty reason: 422 Unprocessable (syntactically valid, fails the business rule), also
// enforced at the storage layer (verification_items_reject_reason_check).
func (s *Service) RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ItemID = strings.TrimSpace(in.ItemID)
	in.Decision = strings.TrimSpace(in.Decision)
	in.Reason = strings.TrimSpace(in.Reason)
	in.VerifierID = strings.TrimSpace(in.VerifierID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.ItemID) {
		return domain.Item{}, BadRequest("invalid_item", "tenant_id and item_id must be UUIDs")
	}
	if !uuidutil.IsUUIDString(in.VerifierID) {
		return domain.Item{}, BadRequest("invalid_verifier", "verifier id must be a UUID")
	}
	if in.Decision != domain.DecisionApproved && in.Decision != domain.DecisionRejected {
		return domain.Item{}, BadRequest("invalid_decision", "decision must be approved or rejected")
	}
	if in.Decision == domain.DecisionRejected && in.Reason == "" {
		return domain.Item{}, Unprocessable("reason_required", "a reason is required to reject a verification item")
	}
	if in.RowVersion < 1 {
		return domain.Item{}, BadRequest("invalid_row_version", "row_version is required")
	}
	if in.IdempotencyKey != "" && (len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 200) {
		return domain.Item{}, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	// The evidence gate guards APPROVE only. Approve is the one irreversible action in this module
	// (there is no un-approve), so it must never be recorded against proof nobody can look at.
	// Reject/rework is deliberately NOT gated: when the proof is gone, sending the work back so the
	// team records it again is the ONLY correct move left, and gating it would strand the verifier
	// with an item she can neither approve nor return.
	if in.Decision == domain.DecisionApproved {
		itemForEvidence, err := s.repo.GetItem(ctx, in.TenantID, in.ItemID)
		if err != nil {
			return domain.Item{}, mapRepoErr(err)
		}
		if err := s.assertEvidenceApprovable(ctx, in.TenantID, itemForEvidence); err != nil {
			return domain.Item{}, err
		}
	}
	item, err := s.repo.RecordVerdict(ctx, in)
	if err != nil {
		return domain.Item{}, mapRepoErr(err)
	}
	return item, nil
}

// assertEvidenceApprovable is the REAL evidence gate for a single approve.
//
// Two layers, because they answer two different questions:
//  1. ResolveMedia — can a signed link be issued for every media_ref? (row-level completeness)
//  2. EnsureEvidenceAvailable — do the stored objects still EXIST? (byte-level truth)
//
// Layer 2 is the one that matters and the one that used to be missing: RecordVerdict called the
// same non-statting resolver the queue read uses, so the gate was a tautology — if the DB row
// existed it passed, and an approve could be recorded against an object that had been deleted or
// relocated (the download route then answers 410 proof_object_missing to a verifier who has
// already, irreversibly, approved it).
//
// The N+1 objection that ListQueue/resolveMedia correctly raises does NOT apply here: this is ONE
// item at decision time, not ~20 rows x ~3 proofs on a hot read. Paying a handful of stats once,
// before an irreversible act nobody can undo, is the correct trade. Do not move this into the
// queue path, and do not delete it to "make approve faster".
func (s *Service) assertEvidenceApprovable(ctx context.Context, tenantID string, item domain.Item) error {
	if s.media == nil || len(item.MediaRefs) == 0 {
		return evidenceMissingErr()
	}
	resolved, err := s.media.ResolveMedia(ctx, tenantID, item.MediaRefs)
	if err != nil || len(resolved) != len(item.MediaRefs) {
		return evidenceMissingErr()
	}
	checker, ok := s.media.(ports.EvidenceAvailabilityChecker)
	if !ok {
		// No adapter can confirm the bytes. Fail CLOSED on the irreversible action rather than
		// repeat the old tautology, and say so honestly: this is "we could not check", not "the
		// video is gone".
		return evidenceUncheckableErr()
	}
	switch err := checker.EnsureEvidenceAvailable(ctx, tenantID, item.MediaRefs); {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrEvidenceMissing):
		return evidenceMissingErr()
	default:
		return evidenceUncheckableErr()
	}
}

// evidenceMissingErr is terminal: the proof video is not there and retrying cannot change that.
// The copy tells her the one thing she can still do — send it back so the team records it again.
func evidenceMissingErr() *Error {
	return Unprocessable(
		"evidence_missing",
		"The proof video for this record is not there, so it cannot be approved. Send it back for rework so the team records it again.",
	)
}

// evidenceUncheckableErr is NOT proof of absence — the check itself did not complete, so it is
// retryable and must not accuse the operator of losing the video.
func evidenceUncheckableErr() *Error {
	err := Unprocessable(
		"evidence_check_failed",
		"The proof video could not be opened just now, so it cannot be approved yet. Try again in a moment, or send it back for rework.",
	)
	err.Retryable = true
	return err
}

// CloseItem applies the leadership action after independent verifier approval. The owning module
// consumes verification.item.closed to apply its business transition; the verifier timestamp is
// never substituted for the operator's administered_at.
func (s *Service) CloseItem(ctx context.Context, in domain.CloseAction) (domain.Item, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ItemID = strings.TrimSpace(in.ItemID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.ItemID) {
		return domain.Item{}, BadRequest("invalid_item", "tenant_id and item_id must be UUIDs")
	}
	if !uuidutil.IsUUIDString(in.ActorID) {
		return domain.Item{}, BadRequest("invalid_actor", "actor id must be a UUID")
	}
	if in.RowVersion < 1 {
		return domain.Item{}, BadRequest("invalid_row_version", "row_version is required")
	}
	if in.IdempotencyKey != "" && (len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 200) {
		return domain.Item{}, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	item, err := s.repo.CloseItem(ctx, in)
	if err != nil {
		return domain.Item{}, mapRepoErr(err)
	}
	return item, nil
}

// CloseSubmission applies one leadership action to the whole operator submission/drive. Storage
// locks the complete item set and fails atomically unless every goat proof is approved.
func (s *Service) CloseSubmission(ctx context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.SubmissionID = strings.TrimSpace(in.SubmissionID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.SubmissionID) {
		return nil, BadRequest("invalid_submission", "tenant_id and submission_id must be UUIDs")
	}
	if !uuidutil.IsUUIDString(in.ActorID) {
		return nil, BadRequest("invalid_actor", "actor id must be a UUID")
	}
	if in.IdempotencyKey != "" && (len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 200) {
		return nil, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	items, err := s.repo.CloseSubmission(ctx, in)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return items, nil
}

// CloseVaccinationBatch is the drive-level leadership close. It is intentionally separate from
// CloseSubmission because a vaccination drive can span multiple planned dates/submissions.
func (s *Service) CloseVaccinationBatch(ctx context.Context, in domain.CloseVaccinationBatchAction) ([]domain.Item, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.BatchID = strings.TrimSpace(in.BatchID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.BatchID) {
		return nil, BadRequest("invalid_batch", "tenant_id and batch_id must be UUIDs")
	}
	if !uuidutil.IsUUIDString(in.ActorID) {
		return nil, BadRequest("invalid_actor", "actor id must be a UUID")
	}
	if in.IdempotencyKey != "" && (len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 200) {
		return nil, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	items, err := s.repo.CloseVaccinationBatch(ctx, in)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return items, nil
}

func (s *Service) GetSubmissionItems(ctx context.Context, tenantID, submissionID string) ([]domain.Item, error) {
	tenantID = strings.TrimSpace(tenantID)
	submissionID = strings.TrimSpace(submissionID)
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(submissionID) {
		return nil, BadRequest("invalid_submission", "tenant_id and submission_id must be UUIDs")
	}
	items, err := s.repo.GetSubmissionItems(ctx, tenantID, submissionID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return items, nil
}

// GetItem fetches one item by id (used by handlers/tests; not directly contract-exposed today).
func (s *Service) GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error) {
	tenantID = strings.TrimSpace(tenantID)
	itemID = strings.TrimSpace(itemID)
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(itemID) {
		return domain.Item{}, BadRequest("invalid_item", "tenant_id and item_id must be UUIDs")
	}
	item, err := s.repo.GetItem(ctx, tenantID, itemID)
	if err != nil {
		return domain.Item{}, mapRepoErr(err)
	}
	return item, nil
}

func boundedLimit(requested, max int) int {
	if requested <= 0 {
		return defaultQueueLimit
	}
	if requested > max {
		return max
	}
	return requested
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("item_not_found", "verification item not found")
	case errors.Is(err, ports.ErrConflict):
		return Conflict("write_conflict", "verification item was modified by someone else")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different request payload")
	default:
		return err
	}
}

// WithdrawItemsBySource is the producing module's retire seam: the module that
// raised the items tells verification that the source records they point at are
// superseded, so the items must stop being decidable. It is not a verdict and is
// not reachable from the verifier-facing HTTP surface.
func (s *Service) WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	sourceModule = strings.TrimSpace(sourceModule)
	sourceRefType = strings.TrimSpace(sourceRefType)
	if !uuidutil.IsUUIDString(tenantID) {
		return 0, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	if sourceModule == "" || sourceRefType == "" {
		return 0, BadRequest("invalid_source_ref", "source module and ref_type are required")
	}
	refs := make([]string, 0, len(sourceRefIDs))
	for _, ref := range sourceRefIDs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if !uuidutil.IsUUIDString(ref) {
			return 0, BadRequest("invalid_source_ref", "source ref_id must be a UUID")
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return 0, nil
	}
	withdrawn, err := s.repo.WithdrawItemsBySource(ctx, tenantID, sourceModule, sourceRefType, refs)
	if err != nil {
		return 0, mapRepoErr(err)
	}
	return withdrawn, nil
}
