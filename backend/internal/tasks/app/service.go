// Package app orchestrates the birth/death follow-up workflow engine: the operator-facing
// list/detail/answer/complete APIs, the workflow openers driven by domain events, and the death
// evidence verification handoff (docs/decisions/birth-death-workflows.md).
package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// DeathVerificationEnqueueRequest is one death evidence pair handed to the verification queue: ONE
// item (category death_evidence) carrying BOTH proofs, so ONE verifier verdict covers the pair.
type DeathVerificationEnqueueRequest struct {
	TenantID   string
	WorkflowID string
	OperatorID string
	ParkID     string
	ShedID     string
	// PartitionLabel is the animal's pen inside ShedID (blank when undivided or unknown).
	PartitionLabel string
	// ProofRefs is the whole bundle in order (capture form first, then steps in seq order; for
	// the seeded document: death video, post mortem video). MediaMeta is positional against it
	// (step title + register kind) and ContextRows carries every answer in farm words grouped by
	// step -- both composed by domain.DeathEvidenceBundle so the release path and the re-shoot
	// path hand the verifier the SAME item.
	ProofRefs    []string
	MediaMeta    []domain.MediaMetaItem
	ContextRows  []domain.EvidenceRow
	SubjectLabel string
	CapturedAt   time.Time
	// IdempotencyKey is "counts-death-evidence:<workflow_id>:r<review round>:<proof refs, in order>",
	// mirroring counts shifting's "counts-shifting-verification:<event>:<proof>".
	//
	// Verification's CreateItem is ON CONFLICT (tenant_id, idempotency_key) DO NOTHING, so a key that
	// does not change between review rounds silently creates NOTHING on the second round: after a
	// verifier rejection the re-shot evidence never reaches a verifier and the workflow is stranded
	// awaiting a verdict that can never arrive. The round (the sign-off's row_version, bumped by every
	// rework and re-submission) guarantees a fresh item even if the operator re-submits byte-identical
	// proof refs; the proofs keep the key describing exactly what is under review. A replay mutates
	// nothing, so round and proofs are both stable and genuine retries still de-duplicate.
	IdempotencyKey string
}

// BirthStepVerificationEnqueueRequest is ONE recorded birth step handed to the verification queue
// (maintainer decision 2026-09-16: birth evidence is reviewed per step, the moment it is recorded,
// and a rejection sends back exactly that step). ref (module=counts,
// ref_type=workflow_birth_action, ref_id=action_id); ProofRefs are that step's own proofs only.
type BirthStepVerificationEnqueueRequest struct {
	Proofs                                                                   []domain.ProofItem
	ProofLabel                                                               string
	TenantID, WorkflowID, ActionID, OperatorID, ParkID, ShedID, SubjectLabel string
	// PartitionLabel is the animal's pen inside ShedID (blank when undivided or unknown).
	PartitionLabel string
	ProofRefs      []string
	// MediaMeta names each ProofRefs entry (step title, numbered only when the step itself holds
	// several of a kind; register-resolved kind) and ContextRows carries the step's answer in
	// farm words. Proofs/ProofLabel are kept for older readers; the bridge renders MediaMeta.
	MediaMeta   []domain.MediaMetaItem
	ContextRows []domain.EvidenceRow
	CapturedAt  time.Time
	// IdempotencyKey is domain.BirthStepReviewKey: action + row_version + proofs, so a retry
	// de-duplicates while a re-shoot after a rejection always opens a fresh item.
	IdempotencyKey string
}

// DeathVerificationEnqueuer is the composition-layer seam to the verification module (the bridge in
// adapters/verificationbridge adapts verification's CreateItem; tasks never writes its tables).
type DeathVerificationEnqueuer interface {
	EnqueueDeathEvidenceVerification(ctx context.Context, in DeathVerificationEnqueueRequest) error
	EnqueueBirthStepVerification(ctx context.Context, in BirthStepVerificationEnqueueRequest) error
}

// ProofKindResolver answers "what kind is this proof" from the PROOF REGISTER (video / photo),
// so a verifier item names the kind the store judged, never the kind a client claimed
// (tasks/adapters/proofkinds adapts proof's repository). A ref the register does not know is
// absent from the map; an error is logged and the client's kind kept -- the register is a
// labelling source, never a gate on a capture.
type ProofKindResolver interface {
	ResolveProofKinds(ctx context.Context, tenantID string, refs []string) (map[string]string, error)
}

// Service is the tasks app service.
type Service struct {
	repo            ports.Repository
	enqueuer        DeathVerificationEnqueuer
	proofKinds      ProofKindResolver
	reshootListener CaptureReshootListener
	now             func() time.Time
	log             *slog.Logger
	completionHooks map[string]WorkflowCompletionHook
}

// NewService constructs the service. now may be nil (defaults to time.Now).
func NewService(repo ports.Repository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, now: time.Now, log: log}
}

// WithVerificationEnqueuer wires the death evidence enqueue seam.
func (s *Service) WithVerificationEnqueuer(enqueuer DeathVerificationEnqueuer) *Service {
	s.enqueuer = enqueuer
	return s
}

// WithProofKindResolver wires the proof register's kind lookup.
func (s *Service) WithProofKindResolver(r ProofKindResolver) *Service {
	s.proofKinds = r
	return s
}

// resolveProofKinds replaces each TYPED proof's kind with the register's verdict where the register
// knows the ref. A bare legacy proof_ref is left exactly as the client sent it -- the client's video,
// as on origin/main: re-kinding it to a photo made a video-required step refuse a capture main
// accepts (2026-09-17 parity revert). Unknown refs and a register error keep what the client sent:
// the register labels, it never blocks.
func (s *Service) resolveProofKinds(ctx context.Context, tenantID, proofRef string, proofs []domain.ProofItem) (string, []domain.ProofItem) {
	if s.proofKinds == nil {
		return proofRef, proofs
	}
	refs := make([]string, 0, len(proofs))
	for _, p := range proofs {
		if r := strings.TrimSpace(p.Ref); r != "" {
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 {
		return proofRef, proofs
	}
	kinds, err := s.proofKinds.ResolveProofKinds(ctx, tenantID, refs)
	if err != nil {
		s.log.Warn("tasks_proof_kind_resolve_failed", "tenant_id", tenantID, "error", err)
		return proofRef, proofs
	}
	out := make([]domain.ProofItem, 0, len(proofs))
	for _, p := range proofs {
		if k, ok := kinds[strings.TrimSpace(p.Ref)]; ok && k != "" {
			p.Kind = k
		}
		out = append(out, p)
	}
	return proofRef, out
}

// WithNow overrides the clock (tests).
func (s *Service) WithNow(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// ListWorkflowsInput is the transport-normalized list request.
type ListWorkflowsInput struct {
	TenantID string
	Module   string
	Date     string // YYYY-MM-DD; empty = today IST
	Filter   string
	PageSize int
	Cursor   string
}

// ListWorkflows serves one keyset page of cards + the day's chips.
func (s *Service) ListWorkflows(ctx context.Context, in ListWorkflowsInput) (domain.WorkflowListPage, error) {
	if strings.TrimSpace(in.TenantID) == "" {
		return domain.WorkflowListPage{}, domain.ErrMissingRequiredField
	}
	if in.Module != domain.ModuleBirth && in.Module != domain.ModuleDeath && in.Module != domain.ModuleGeneral {
		return domain.WorkflowListPage{}, domain.ErrMissingRequiredField
	}
	now := s.now()
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = biztime.BusinessDate(now)
	}
	cursor, err := domain.DecodeWorkflowCursor(in.Cursor)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	return s.repo.ListWorkflows(ctx, domain.WorkflowListQuery{
		TenantID:  in.TenantID,
		Module:    in.Module,
		EventDate: date,
		TodayDate: biztime.BusinessDate(now),
		Filter:    in.Filter,
		PageSize:  in.PageSize,
		Cursor:    cursor,
		Now:       now,
	})
}

// GetWorkflow serves the per-goat detail.
func (s *Service) GetWorkflow(ctx context.Context, tenantID, workflowID string) (domain.WorkflowDetail, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workflowID) == "" {
		return domain.WorkflowDetail{}, domain.ErrMissingRequiredField
	}
	return s.repo.GetWorkflow(ctx, tenantID, workflowID, s.now())
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// AnswerActionInput answers a question / question_select step, including the numeric-kg kid weight.
type AnswerActionInput struct {
	TenantID           string
	WorkflowID         string
	ActionID           string
	AnswerValue        string
	ProofRef           string
	Proofs             []domain.ProofItem
	AnsweredBy         string
	IdempotencyKey     string
	RequestFingerprint string
}

// AnswerAction runs the answer write under the mandatory idempotency contract. An answer that
// finishes the LAST open step of an approved death (a re-answered photo question after a
// rework) re-enters Verify exactly as a completed video does.
func (s *Service) AnswerAction(ctx context.Context, in AnswerActionInput) (domain.ActionWriteResult, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.WorkflowID) == "" ||
		strings.TrimSpace(in.ActionID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ActionWriteResult{}, domain.ErrMissingRequiredField
	}
	proofRef, proofs := s.resolveProofKinds(ctx, in.TenantID, in.ProofRef, in.Proofs)
	result, err := s.repo.AnswerAction(ctx, domain.AnswerActionCommand{
		TenantID:           in.TenantID,
		WorkflowID:         in.WorkflowID,
		ActionID:           in.ActionID,
		AnswerValue:        strings.TrimSpace(in.AnswerValue),
		ProofRef:           strings.TrimSpace(proofRef),
		Proofs:             proofs,
		AnsweredBy:         strings.TrimSpace(in.AnsweredBy),
		AnsweredAt:         s.now().UTC(),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.enqueueBirthStepIfRecorded(ctx, in.TenantID, result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.notifyCompletion(ctx, in.TenantID, result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.enqueueDeathIfReady(ctx, in.TenantID, strings.TrimSpace(in.AnsweredBy), result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	return result, nil
}

// CompleteActionInput completes an "action" step (optionally carrying a video proof).
type CompleteActionInput struct {
	TenantID           string
	WorkflowID         string
	ActionID           string
	ProofRef           string
	Proofs             []domain.ProofItem
	CompletedBy        string
	IdempotencyKey     string
	RequestFingerprint string
}

// deathEvidenceRequest is the ONE builder of a death verifier item, used by the completion path
// (CompleteAction / AnswerAction after an applied death) and by the approval release path
// (ReleaseApprovedDeathEvidence). Before 2026-09-16 the two paths composed different refs from
// different columns, so a redelivered goat.exited could open a second item for the same round.
// The bundle order, meta, rows and the round-keyed idempotency key are shared, so they cannot.
func (s *Service) deathEvidenceRequest(ctx context.Context, tenantID string, w domain.WorkflowInstance, actions []domain.WorkflowAction, operatorID string, capturedAt time.Time) DeathVerificationEnqueueRequest {
	bundle := domain.DeathEvidenceBundle(w.CaptureEvidence, actions)
	shedID := derefOr(w.ShedID)
	shedName, partitionLabel, _ := s.repo.FetchShedDetails(ctx, tenantID, shedID)
	// The animal's display id names WHICH death when two are recorded in the same pen on the same
	// day (edge-case audit 2026-09-18: "Death evidence · 18/09/2026 · Gandhi 1" twice over is
	// undecidable for the verifier). A lookup failure degrades to the date and pen alone.
	subject := "Death evidence"
	if facts, err := s.repo.GoatWorkflowFacts(ctx, tenantID, w.SubjectGoatID); err == nil {
		if id := strings.TrimSpace(facts.DisplayID); id != "" {
			subject += " · " + id
		}
		if pen := penInShed(facts, shedID); pen != "" {
			partitionLabel = pen
		}
	}
	if capturedAt.IsZero() {
		capturedAt = s.now().UTC()
	}
	return DeathVerificationEnqueueRequest{
		TenantID:       tenantID,
		WorkflowID:     w.WorkflowID,
		OperatorID:     strings.TrimSpace(operatorID),
		ParkID:         derefOr(w.ParkID),
		ShedID:         shedID,
		PartitionLabel: partitionLabel,
		ProofRefs:      bundle.Refs,
		MediaMeta:      bundle.Meta,
		ContextRows:    bundle.Rows,
		SubjectLabel:   appendLocation(subject+" · "+biztime.FarmDateFromBusinessDate(w.EventDate), shedName, partitionLabel),
		CapturedAt:     capturedAt,
		IdempotencyKey: domain.DeathEvidenceKey(w.WorkflowID, w.RowVersion, bundle.Refs),
	}
}

// enqueueDeathIfReady hands the bundle to the verifier when a write left an already-applied
// death awaiting its verdict (NeedsVerificationEnqueue is derived from STATE, so an exact
// replay after a failed enqueue re-reports it and CreateItem's round+refs key makes the retry
// heal rather than duplicate).
func (s *Service) enqueueDeathIfReady(ctx context.Context, tenantID, operatorID string, result domain.ActionWriteResult) error {
	if !result.NeedsVerificationEnqueue {
		return nil
	}
	if s.enqueuer == nil {
		// Composition bug, surfaced loudly. The completion is durable; wiring the enqueuer and
		// replaying the same request heals (state-derived NeedsVerificationEnqueue + idempotent
		// CreateItem).
		return domain.ErrVerificationEnqueuerNotWired
	}
	detail, err := s.repo.GetWorkflow(ctx, tenantID, result.Workflow.WorkflowID, s.now())
	if err != nil {
		return err
	}
	w := result.Workflow
	w.RowVersion = result.DeathReviewRound
	return s.enqueuer.EnqueueDeathEvidenceVerification(ctx, s.deathEvidenceRequest(ctx, tenantID, w, detail.Actions, operatorID, s.now().UTC()))
}

// CompleteAction runs the complete write. The transactional ordering mirrors counts shifting: the
// durable completion commits FIRST, then (same request) the verification item is enqueued when the
// death sign-off is now awaiting review. An enqueue failure never loses the completion — the write
// is committed and NeedsVerificationEnqueue is derived from STATE, so an idempotent retry (exact
// replay) re-enqueues; CreateItem is idempotent on the workflow+round+proofs key, so retries heal
// rather than duplicate while a post-rejection re-shoot opens a fresh review item.
func (s *Service) CompleteAction(ctx context.Context, in CompleteActionInput) (domain.ActionWriteResult, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.WorkflowID) == "" ||
		strings.TrimSpace(in.ActionID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ActionWriteResult{}, domain.ErrMissingRequiredField
	}
	proofRef, proofs := s.resolveProofKinds(ctx, in.TenantID, in.ProofRef, in.Proofs)
	result, err := s.repo.CompleteAction(ctx, domain.CompleteActionCommand{
		TenantID:           in.TenantID,
		WorkflowID:         in.WorkflowID,
		ActionID:           in.ActionID,
		ProofRef:           strings.TrimSpace(proofRef),
		Proofs:             proofs,
		CompletedBy:        strings.TrimSpace(in.CompletedBy),
		CompletedAt:        s.now().UTC(),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.enqueueBirthStepIfRecorded(ctx, in.TenantID, result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.notifyCompletion(ctx, in.TenantID, result); err != nil {
		return domain.ActionWriteResult{}, err
	}

	if err := s.enqueueDeathIfReady(ctx, in.TenantID, strings.TrimSpace(in.CompletedBy), result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.notifyCaptureReshoot(ctx, in.TenantID, result); err != nil {
		return domain.ActionWriteResult{}, err
	}
	return result, nil
}

// enqueueBirthStepIfRecorded hands ONE just-recorded birth step to the verifier. The step's
// durable write committed first (it is now in_review); the enqueue is derived from that STATE, so
// an exact replay after a failed enqueue re-reports it and CreateItem's idempotency on the
// per-recording key makes the retry heal rather than duplicate. Nothing waits for the rest of the
// track: the verifier sees the clip the moment the operator records it.
func (s *Service) enqueueBirthStepIfRecorded(ctx context.Context, tenantID string, result domain.ActionWriteResult) error {
	if !result.NeedsStepReviewEnqueue {
		return nil
	}
	if s.enqueuer == nil {
		return domain.ErrVerificationEnqueuerNotWired
	}
	w, step := result.Workflow, result.Action
	subject := "Kid"
	if w.TemplateKey == domain.TemplateKeyBirthMother {
		subject = "Mother"
	}
	// The animal's display id names WHICH kid when a litter has twins recording the same step on
	// the same day in the same pen; a lookup failure degrades to the role alone rather than
	// blocking the enqueue.
	shedID := derefOr(w.ShedID)
	partitionLabel := ""
	if facts, err := s.repo.GoatWorkflowFacts(ctx, tenantID, w.SubjectGoatID); err == nil {
		if strings.TrimSpace(facts.DisplayID) != "" {
			subject += " " + strings.TrimSpace(facts.DisplayID)
		}
		partitionLabel = penInShed(facts, shedID)
	}
	capturedAt := s.now().UTC()
	if step.CompletedAt != nil {
		capturedAt = step.CompletedAt.UTC()
	}
	// The answer in farm words: a Record pen step names a location, resolved through the
	// canonical fetch so the verifier reads "Godel 1 - Part 3", never "<uuid>|Part 3".
	penDisplay := ""
	if step.HasHook(domain.EngineHookRecordPen) && step.AnswerValue != nil {
		if penShed, label, err := domain.ParseRecordedPenAnswer(*step.AnswerValue); err == nil {
			if name, _, err := s.repo.FetchShedDetails(ctx, tenantID, penShed); err == nil {
				penDisplay = composeOperationalLocation(name, label)
			}
		}
	}
	var rows []domain.EvidenceRow
	if row, ok := domain.StepAnswerRow(step, penDisplay); ok {
		rows = []domain.EvidenceRow{row}
	}
	// The label names the STEP and the ANIMAL only. The pen is carried on the item's own
	// shed_id/partition and both verifier surfaces render it themselves (the web in its Pen
	// column, the phone appended to the title), so a pen inside the label rendered twice
	// ("... · Yashoda 1 · Yashoda 1") on the 2026-09-16 phone run.
	return s.enqueuer.EnqueueBirthStepVerification(ctx, BirthStepVerificationEnqueueRequest{
		TenantID: tenantID, WorkflowID: w.WorkflowID, ActionID: step.ActionID,
		OperatorID: derefOr(step.CompletedBy), ParkID: derefOr(w.ParkID), ShedID: shedID,
		PartitionLabel: partitionLabel,
		ProofRefs:      step.AllProofRefs(),
		Proofs:         step.ProofRefs, ProofLabel: step.Title,
		MediaMeta: domain.StepMediaMeta(step), ContextRows: rows,
		SubjectLabel:   step.Title + " · " + subject + " · " + biztime.FarmDateFromBusinessDate(w.EventDate),
		CapturedAt:     capturedAt,
		IdempotencyKey: domain.BirthStepReviewKey(step),
	})
}

// ---------------------------------------------------------------------------
// Workflow openers (event-driven)
// ---------------------------------------------------------------------------

// OpenBirthWorkflowsInput opens the kid track (and the shared mother track when the dam resolves)
// after a birth-origin goat.created applied.
type OpenBirthWorkflowsInput struct {
	TenantID string
	GoatID   string
	// DamRef is the goat.created payload's dam_id: free text (a tag) or a uuid. Optional.
	DamRef string
	// PayloadTimeOfBirth is the payload's time_of_birth (HH:MM IST). The canonical goats row wins
	// when it carries one; this is the fallback for consumers racing an older row shape.
	PayloadTimeOfBirth string
	// OccurredAt anchors the fallback event date when the canonical row has no DOB.
	OccurredAt time.Time
}

// OpenBirthWorkflows reads the canonical goat row (ONE indexed PK lookup) and opens the birth_kid
// workflow; when the dam resolves to a canonical animal it also opens (or attaches to) the shared
// birth_mother workflow. Idempotent: the workflow natural key absorbs event redelivery and twins.
func (s *Service) OpenBirthWorkflows(ctx context.Context, in OpenBirthWorkflowsInput) error {
	facts, err := s.repo.GoatWorkflowFacts(ctx, in.TenantID, in.GoatID)
	if err != nil {
		return err
	}
	eventAt := birthMoment(facts.DOB, facts.TimeOfBirth, in.PayloadTimeOfBirth, in.OccurredAt)
	damRef := strings.TrimSpace(in.DamRef)
	var damGoatID string
	if damRef != "" {
		damGoatID, err = s.repo.ResolveDamGoat(ctx, in.TenantID, damRef)
		if err != nil {
			if err != domain.ErrNotFound {
				return err
			}
			// Legacy goat.created events may predate the required canonical mother contract. Keep
			// opening their kid track, but do not fabricate a relationship.
			s.log.Info("tasks_birth_mother_dam_unresolved", "tenant_id", in.TenantID, "goat_id", in.GoatID, "dam_ref", damRef)
			damGoatID = ""
		}
	}
	var kidMotherID *string
	if damGoatID != "" {
		kidMotherID = &damGoatID
	}
	if _, err := s.repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:      in.TenantID,
		TemplateKey:   domain.TemplateKeyBirthKid,
		SubjectGoatID: in.GoatID,
		DamGoatID:     kidMotherID,
		EventAt:       eventAt,
		ParkID:        facts.ParkID,
		ShedID:        facts.ShedID,
	}); err != nil {
		return err
	}

	if damGoatID == "" {
		return nil
	}
	damFacts, err := s.repo.GoatWorkflowFacts(ctx, in.TenantID, damGoatID)
	if err != nil {
		return err
	}
	kidID := in.GoatID
	_, err = s.repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:      in.TenantID,
		TemplateKey:   domain.TemplateKeyBirthMother,
		SubjectGoatID: damGoatID,
		DamGoatID:     &kidID, // on the mother track this links back to the (first) kid
		EventAt:       eventAt,
		ParkID:        damFacts.ParkID,
		ShedID:        damFacts.ShedID,
	})
	return err
}

// OpenDeathWorkflowInput opens the staged death evidence trail from counts.death.reported.
type OpenDeathWorkflowInput struct {
	TenantID string
	GoatID   string
	ParkID   string
	ShedID   string
	// OccurredAt is the exit moment (the death event's business anchor).
	OccurredAt time.Time
	// CaptureEvidence is the Add death form's snapshot from counts.death.reported; it is stamped
	// on the instance at open and leads the verifier bundle.
	CaptureEvidence authored.Evidence
}

// OpenDeathWorkflow opens the death workflow. Idempotent on the natural key.
func (s *Service) OpenDeathWorkflow(ctx context.Context, in OpenDeathWorkflowInput) error {
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = s.now()
	}
	_, err := s.repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:        in.TenantID,
		TemplateKey:     domain.TemplateKeyDeath,
		SubjectGoatID:   in.GoatID,
		EventAt:         occurred,
		ParkID:          optionalUUID(in.ParkID),
		ShedID:          optionalUUID(in.ShedID),
		CaptureEvidence: in.CaptureEvidence,
	})
	return err
}

// OpenReportedDeathWorkflow reads the still-live goat's canonical placement and opens the
// SOP's death steps as soon as the death report is submitted, before any admin decision. The
// report's capture snapshot is stamped on the instance so the verifier bundle leads with it.
func (s *Service) OpenReportedDeathWorkflow(ctx context.Context, tenantID, goatID string, reportedAt time.Time, capture authored.Evidence) error {
	facts, err := s.repo.GoatWorkflowFacts(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	return s.OpenDeathWorkflow(ctx, OpenDeathWorkflowInput{
		TenantID: tenantID, GoatID: goatID,
		ParkID: derefOr(facts.ParkID), ShedID: derefOr(facts.ShedID), OccurredAt: reportedAt,
		CaptureEvidence: capture,
	})
}

// ReleaseApprovedDeathEvidence is called by the approved death's goat.exited event. The approval
// transaction has already proven every authored step and opened the verification gate; this
// method performs the idempotent cross-module enqueue through the SAME builder the completion
// path uses. Event redelivery heals a transient enqueue failure without duplicating the item.
func (s *Service) ReleaseApprovedDeathEvidence(ctx context.Context, tenantID, goatID string, capturedAt time.Time) error {
	review, err := s.repo.DeathEvidenceForVerification(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	if s.enqueuer == nil {
		return domain.ErrVerificationEnqueuerNotWired
	}
	return s.enqueuer.EnqueueDeathEvidenceVerification(ctx,
		s.deathEvidenceRequest(ctx, tenantID, review.Workflow, review.Actions, review.OperatorID, capturedAt))
}

// CancelRejectedDeathWorkflow removes rejected reports from operator work without changing the
// goat. The admin decision remains visible in approval history through the existing web behavior.
func (s *Service) CancelRejectedDeathWorkflow(ctx context.Context, tenantID, goatID string, at time.Time) error {
	return s.repo.CancelDeathWorkflowForGoat(ctx, tenantID, goatID, at)
}

// CompleteTagAction records the permanent-RFID prerequisite when the identifier event lands.
// It never completes or enqueues the task; the mandatory tagging-video command does that.
func (s *Service) CompleteTagAction(ctx context.Context, tenantID, goatID string, at time.Time) error {
	if at.IsZero() {
		at = s.now().UTC()
	}
	return s.repo.CompleteTagActionForGoat(ctx, tenantID, goatID, at)
}

// ApplyDeathSignoffApproved / BounceDeathVideosForRework apply a verifier's verdict.
func (s *Service) ApplyDeathSignoffApproved(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	return s.ackUnroutableVerdict("approved", cmd, s.repo.ApplyDeathSignoffApproved(ctx, cmd))
}

func (s *Service) BounceDeathVideosForRework(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	return s.ackUnroutableVerdict("rework", cmd, s.repo.BounceDeathVideosForRework(ctx, cmd))
}

// ApplyBirthStepVerdict lands one verifier verdict on one recorded birth step: approve completes
// that step, reject sends exactly that step back with the verifier's words. A verdict whose
// action no longer exists is acked (logged), never retried.
func (s *Service) ApplyBirthStepVerdict(ctx context.Context, cmd ports.BirthStepVerdictCommand) error {
	err := s.repo.ApplyBirthStepVerdict(ctx, cmd)
	if errors.Is(err, domain.ErrNotFound) {
		s.log.Warn("tasks_birth_step_verdict_unroutable", "approved", cmd.Approved, "tenant_id", cmd.TenantID, "action_id", cmd.ActionID)
		return nil
	}
	return err
}

// ApplyBirthSignoffApproved / BounceBirthVideoForRework serve the retired whole-workflow bundle.
func (s *Service) ApplyBirthSignoffApproved(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	return s.ackUnroutableBirthVerdict("approved", cmd, s.repo.ApplyBirthSignoffApproved(ctx, cmd))
}

func (s *Service) BounceBirthVideoForRework(ctx context.Context, cmd ports.DeathVerdictCommand) error {
	return s.ackUnroutableBirthVerdict("rework", cmd, s.repo.BounceBirthVideoForRework(ctx, cmd))
}

func (s *Service) ackUnroutableBirthVerdict(verdict string, cmd ports.DeathVerdictCommand, err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		s.log.Warn("tasks_birth_verdict_unroutable", "verdict", verdict, "tenant_id", cmd.TenantID, "workflow_id", cmd.WorkflowID)
		return nil
	}
	return err
}

// ackUnroutableVerdict acks a verdict that addresses no death workflow instead of retrying it
// forever, but LOGS it first. A redelivered verdict is a no-op mutation rather than ErrNotFound, so
// reaching here means the item's ref_id does not resolve — a routing defect that would otherwise be
// indistinguishable from a benign replay.
func (s *Service) ackUnroutableVerdict(verdict string, cmd ports.DeathVerdictCommand, err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		s.log.Warn("tasks_death_verdict_unroutable",
			"verdict", verdict, "tenant_id", cmd.TenantID, "workflow_id", cmd.WorkflowID)
		return nil
	}
	return err
}

// birthMoment resolves the birth event moment on the India business calendar: the DOB's date at the
// recorded time_of_birth (canonical goats.time_of_birth first, payload fallback), or 07:00 IST when
// the time is unknown. A goat with no DOB anchors on the event's own business date.
func birthMoment(dob *time.Time, rowTime *string, payloadTime string, occurredAt time.Time) time.Time {
	loc := biztime.DefaultLocation()
	day := biztime.BusinessDayStart(occurredAt)
	if dob != nil {
		day = biztime.BusinessDayStart(*dob)
	}
	hour, minute := 7, 0
	tob := ""
	if rowTime != nil && strings.TrimSpace(*rowTime) != "" {
		tob = strings.TrimSpace(*rowTime)
	} else if strings.TrimSpace(payloadTime) != "" {
		tob = strings.TrimSpace(payloadTime)
	}
	if tob != "" {
		if parsed, err := time.Parse("15:04", tob); err == nil {
			hour, minute = parsed.Hour(), parsed.Minute()
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
}

// animalPen is the subject animal's pen inside the workflow's shed ("" when unknown).
func (s *Service) animalPen(ctx context.Context, tenantID string, w domain.WorkflowInstance) string {
	facts, err := s.repo.GoatWorkflowFacts(ctx, tenantID, w.SubjectGoatID)
	if err != nil {
		return ""
	}
	return penInShed(facts, derefOr(w.ShedID))
}

// penInShed names the animal's partition only while the animal stands in that shed: a verifier
// item scoped to shed A must never carry a pen of shed B (E2E 2026-09-17).
func penInShed(facts ports.GoatWorkflowFacts, shedID string) string {
	if shedID == "" || facts.ShedID == nil || strings.TrimSpace(*facts.ShedID) != shedID {
		return ""
	}
	return strings.TrimSpace(facts.PartitionLabel)
}

// composeOperationalLocation composes a location display string from shed name and partition label.
func composeOperationalLocation(shedName, partitionLabel string) string {
	loc := oploc.OperationalLocation{
		ShedName:       shedName,
		PartitionLabel: partitionLabel,
	}
	return loc.Display()
}

// appendLocation adds " · <location>" to a subject label ONLY when the location resolves.
//
// An unresolvable shed must DEGRADE to the location-less label. Concatenating unconditionally
// yields "Death evidence · 2026-08-07 · " -- a dangling separator in an operator's queue. That
// is the same class as the `Raised by <uuid>` copy defect: a label composed from a value that
// was not there.
func appendLocation(label, shedName, partitionLabel string) string {
	loc := composeOperationalLocation(shedName, partitionLabel)
	if strings.TrimSpace(loc) == "" {
		return label
	}
	return label + " · " + loc
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optionalUUID(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
