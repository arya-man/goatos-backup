// Package app implements the Verification module's use-cases: producers enqueue items, a Verifier
// lists their category-filtered queue and records an approve/reject verdict.
package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	params.BusinessDate = strings.TrimSpace(params.BusinessDate)
	params.ParkID = strings.TrimSpace(params.ParkID)
	params.ShedID = strings.TrimSpace(params.ShedID)
	if params.ParkID != "" && !uuidutil.IsUUIDString(params.ParkID) {
		return QueueResult{}, BadRequest("invalid_park", "park_id must be a UUID")
	}
	if params.ShedID != "" && !uuidutil.IsUUIDString(params.ShedID) {
		return QueueResult{}, BadRequest("invalid_shed", "shed_id must be a UUID")
	}
	todayStart := biztime.BusinessDayStart(s.now())
	params.MissedBefore = &todayStart
	if params.MissedOnly {
		if params.BusinessDate != "" {
			return QueueResult{}, BadRequest("invalid_date_scope", "business_date and missed cannot be combined")
		}
		if params.Status != "" && params.Status != domain.StatusPending {
			return QueueResult{}, BadRequest("invalid_missed_status", "missed verification items must use pending status")
		}
		params.Status = domain.StatusPending
		params.CapturedBefore = &todayStart
	} else if params.BusinessDate == "" && !params.IncludeAllStatuses && !params.IsVerifierQueueRead {
		// For non-verifier queue reads, default BusinessDate to today. For verifier queue reads
		// (verification.review path), do NOT clamp to today — return the full pending backlog
		// ordered oldest-first.
		params.BusinessDate = biztime.BusinessDate(s.now())
	}
	if params.BusinessDate != "" {
		parsed, err := time.ParseInLocation("2006-01-02", params.BusinessDate, biztime.DefaultLocation())
		if err != nil {
			return QueueResult{}, BadRequest("invalid_business_date", "business_date must be YYYY-MM-DD")
		}
		if parsed.After(todayStart) {
			return QueueResult{}, BadRequest("future_business_date", "business_date cannot be in the future")
		}
		before := parsed.AddDate(0, 0, 1)
		params.CapturedFrom = &parsed
		params.CapturedBefore = &before
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
	if options.Parks == nil {
		options.Parks = []domain.LocationFilterOption{}
	}
	if options.Sheds == nil {
		options.Sheds = []domain.LocationFilterOption{}
	}
	options.ActionTypes = s.actionTypeOptions()
	options.ModuleKey, options.ModuleLabel, options.Pages = s.pageOptions(params.Category)
	options.Statuses = []domain.QueueStatusOption{
		// "All" applies NO status predicate: the repository's status filter is already
		// `($n = '' OR vi.status = $n)`, so an empty status returns due, approved and rejected
		// together. It leads the list because an operator scanning Actions wants the whole
		// picture first (maintainer request 2026-07-30).
		{Key: "all", Label: "All"},
		{Key: "due", Label: "To verify", Status: domain.StatusPending},
		{Key: "approved", Label: "Accepted", Status: domain.StatusApproved},
		{Key: "rejected", Label: "Rejected", Status: domain.StatusRejected},
	}
	options.SelectedBusinessDate = params.BusinessDate
	options.BusinessTimezone = biztime.DefaultTimezone
	options.MissedOnly = params.MissedOnly
	return QueueResult{Items: s.resolveMedia(ctx, params.TenantID, items), FilterOptions: options, NextCursor: next}, nil
}

// actionTypeOptions returns every registered verification page as a stable, cross-module filter
// vocabulary. Unlike pageOptions, this list is intentionally independent of the selected category
// and current queue rows so an empty page never makes an action type disappear from admin-web.
func (s *Service) actionTypeOptions() []domain.QueueActionTypeOption {
	definitions := s.registry.List()
	sort.SliceStable(definitions, func(i, j int) bool {
		if definitions[i].NavigationModuleLabel != definitions[j].NavigationModuleLabel {
			return definitions[i].NavigationModuleLabel < definitions[j].NavigationModuleLabel
		}
		if definitions[i].PageOrder != definitions[j].PageOrder {
			return definitions[i].PageOrder < definitions[j].PageOrder
		}
		return definitions[i].Category < definitions[j].Category
	})
	options := make([]domain.QueueActionTypeOption, 0, len(definitions))
	for _, def := range definitions {
		if def.NavigationModule == "" || def.NavigationModuleLabel == "" || def.PageLabel == "" {
			continue
		}
		options = append(options, domain.QueueActionTypeOption{
			Key: def.Category, Label: def.PageLabel, Category: def.Category,
			ModuleKey: def.NavigationModule, ModuleLabel: def.NavigationModuleLabel,
		})
	}
	return options
}

// pageOptions derives the selected module's complete top-tab set from the category registry,
// including pages with no current queue rows. This keeps tab presence stable when a queue is
// empty and makes the backend the sole owner of page keys, labels, ordering, and filters.
func (s *Service) pageOptions(selectedCategory string) (string, string, []domain.QueuePageOption) {
	selected, ok := s.registry.Get(selectedCategory)
	if !ok || selected.NavigationModule == "" {
		return "", "", []domain.QueuePageOption{}
	}
	definitions := s.registry.List()
	sort.SliceStable(definitions, func(i, j int) bool {
		if definitions[i].PageOrder == definitions[j].PageOrder {
			return definitions[i].PageKey < definitions[j].PageKey
		}
		return definitions[i].PageOrder < definitions[j].PageOrder
	})
	pages := make([]domain.QueuePageOption, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, def := range definitions {
		if def.NavigationModule != selected.NavigationModule || def.PageKey == "" {
			continue
		}
		if _, exists := seen[def.PageKey]; exists {
			continue
		}
		seen[def.PageKey] = struct{}{}
		pages = append(pages, domain.QueuePageOption{Key: def.PageKey, Label: def.PageLabel, Category: def.Category})
	}
	return selected.NavigationModule, selected.NavigationModuleLabel, pages
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
// Resolution is per-item: an item whose own media refs all resolve keeps its media and
// evidence_available=true; an item with any unresolvable ref of its OWN gets empty media +
// evidence_available=false. The resolver reports per-ID failures as empty MediaItems with DownloadURL="",
// which the service layer detects to fail-close only that item (not the whole page).
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
	if len(allProofIDs) > 0 && s.media != nil {
		resolved, err := s.media.ResolveMedia(ctx, tenantID, allProofIDs)
		if err == nil && len(resolved) == len(allProofIDs) {
			for _, m := range resolved {
				mediaByID[m.ProofID] = m
			}
		}
		// On error, mediaByID stays empty; all items fail-close below.
	}
	for i, it := range items {
		media := make([]domain.MediaItem, 0, len(it.MediaRefs))
		allResolved := true
		for _, id := range it.MediaRefs {
			if m, ok := mediaByID[id]; ok && m.DownloadURL != "" {
				media = append(media, m)
			} else {
				// This item's ref did not resolve (not in mediaByID or has empty URL)
				allResolved = false
			}
		}
		labelMedia(media, s.categoryFor(it.Category))
		// evidence_available is true ONLY when this item's own refs all resolved AND there is actual media to show
		rows[i] = domain.QueueRow{Item: it, Media: media, EvidenceLinkResolved: allResolved && len(media) > 0}
	}
	return rows
}

// categoryFor returns the registered definition for a category, or a zero definition when the
// category predates its registry entry — MediaLabelFor still yields a numbered header from that.
func (s *Service) categoryFor(category string) domain.CategoryDefinition {
	if s.registry == nil {
		return domain.CategoryDefinition{}
	}
	def, _ := s.registry.Get(category)
	return def
}

// labelMedia guarantees every proof carries a header before it reaches a renderer.
//
// A label already resolved from workflow task truth is the most specific thing available and is
// left alone; only blanks are filled from the category's registry copy. Doing this here rather than
// in the client is what keeps the rule in verifier-app-and-flow.md true — the backend owns the
// label and the client never derives one from category or list position.
func labelMedia(media []domain.MediaItem, def domain.CategoryDefinition) {
	for i := range media {
		if strings.TrimSpace(media[i].Label) != "" {
			continue
		}
		media[i].Label = def.MediaLabelFor(i, len(media))
	}
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
	if in.Decision == domain.DecisionApproved {
		s.autoCloseSubmissionWhenFullyApproved(ctx, in, item)
	}
	return item, nil
}

// autoCloseSubmissionWhenFullyApproved closes the SHED as soon as its last animal is approved.
//
// Proof is per animal, so a shed's evidence is now several verification items. Making the
// verifier approve each animal and THEN perform a separate shed-level close would be a second
// action carrying no extra judgement -- she already said yes to every animal in it. So the
// submission closes itself on the approval that completes the set.
//
// Deliberately best-effort and non-fatal: the verdict is already committed and durable, and the
// operator's animal is decided either way. If the close loses a race or fails, the leadership
// close path still works exactly as before, so a failure here degrades to "not auto-closed",
// never to a lost or half-applied verdict.
//
// Replay-safe on three counts: the repository's CloseSubmission locks the whole item set and
// refuses unless every item is approved; the idempotency key is derived from the submission, so
// a duplicated approve resolves to the same close; and an already-closed submission has no
// still-approved-but-open item left to close.
func (s *Service) autoCloseSubmissionWhenFullyApproved(ctx context.Context, in domain.Verdict, item domain.Item) {
	submissionID := ""
	if item.Source.SubmissionID != nil {
		submissionID = strings.TrimSpace(*item.Source.SubmissionID)
	}
	if !uuidutil.IsUUIDString(submissionID) {
		return
	}
	siblings, err := s.repo.GetSubmissionItems(ctx, in.TenantID, submissionID)
	if err != nil || len(siblings) == 0 {
		return
	}
	for _, sibling := range siblings {
		// Withdrawn items are retractions, not outstanding work, and must not hold the shed open.
		if sibling.Status == domain.StatusWithdrawn {
			continue
		}
		if sibling.Status != domain.StatusApproved {
			return
		}
		if sibling.ClosedAt != nil {
			// Already closed by this path or by leadership; nothing left to do.
			return
		}
	}
	_, _ = s.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID:       in.TenantID,
		SubmissionID:   submissionID,
		ActorID:        in.VerifierID,
		IdempotencyKey: "verification:auto-close:submission:" + submissionID,
	})
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
	var notVerified *ports.ErrBatchNotFullyVerified
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return NotFound("item_not_found", "verification item not found")
	case errors.As(err, &notVerified):
		return Conflict("batch_not_fully_verified", batchNotFullyVerifiedMessage(notVerified.Blocking))
	case isAlreadyDecided(err):
		// Terminal, not contended: retrying cannot help, so say the decision is final.
		return Conflict("already_decided", "This proof already has a verdict and cannot be changed.")
	case errors.Is(err, ports.ErrConflict):
		return Conflict("write_conflict", "verification item was modified by someone else")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different request payload")
	default:
		return err
	}
}

// batchNotFullyVerifiedMessage renders CloseVaccinationBatch's named refusal: "3 animals still
// awaiting verification: G-00X, G-00Y, G-00Z" so the UI has something to show the CEO/director
// directly, instead of a bare "write conflict".
func batchNotFullyVerifiedMessage(blocking []string) string {
	n := len(blocking)
	switch {
	case n == 0:
		return "This drive cannot be closed: one or more animals are still awaiting verification."
	case n == 1:
		return fmt.Sprintf("1 animal still awaiting verification: %s", blocking[0])
	default:
		return fmt.Sprintf("%d animals still awaiting verification: %s", n, strings.Join(blocking, ", "))
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

// MarkVerdictApplied is the producing module's APPLY-RECEIPT seam, the mirror of
// the retire seam above: the module that raised the items tells verification that
// its applier has written the verdict's outcome onto its own record, so the item
// stops reading as decided-but-not-yet-in-effect.
//
// Like the retire seam it is not a verdict and is not reachable from the
// verifier-facing HTTP surface -- only a module's own applier may ack its own
// items, and it may only ack that something happened, never what.
func (s *Service) MarkVerdictApplied(
	ctx context.Context,
	tenantID, sourceModule, sourceRefType string,
	sourceRefIDs []string,
	appliedByModule string,
) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	sourceModule = strings.TrimSpace(sourceModule)
	sourceRefType = strings.TrimSpace(sourceRefType)
	appliedByModule = strings.TrimSpace(appliedByModule)
	if !uuidutil.IsUUIDString(tenantID) {
		return 0, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	if sourceModule == "" || sourceRefType == "" {
		return 0, BadRequest("invalid_source_ref", "source module and ref_type are required")
	}
	if appliedByModule == "" {
		return 0, BadRequest("invalid_applied_by_module", "applied_by_module is required")
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
	applied, err := s.repo.MarkVerdictApplied(ctx, tenantID, sourceModule, sourceRefType, refs, appliedByModule)
	if err != nil {
		return 0, mapRepoErr(err)
	}
	return applied, nil
}

// isAlreadyDecided keeps mapRepoErr readable; AlreadyDecidedError carries the current status
// rather than being a bare sentinel, so errors.Is has nothing to match against.
func isAlreadyDecided(err error) bool {
	decided := &ports.AlreadyDecidedError{}
	return errors.As(err, &decided)
}
