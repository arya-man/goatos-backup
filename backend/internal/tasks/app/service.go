// Package app orchestrates the birth/death follow-up workflow engine: the operator-facing
// list/detail/answer/complete APIs, the workflow openers driven by domain events, and the death
// evidence verification handoff (docs/decisions/birth-death-workflows.md).
package app

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// DeathVerificationEnqueueRequest is one death evidence pair handed to the verification queue: ONE
// item (category death_evidence) carrying BOTH proofs, so ONE verifier verdict covers the pair.
type DeathVerificationEnqueueRequest struct {
	TenantID     string
	WorkflowID   string
	OperatorID   string
	ParkID       string
	ShedID       string
	ProofRefs    []string // ordered: death video, post mortem video
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

type BirthVerificationEnqueueRequest struct {
	TenantID, WorkflowID, OperatorID, ParkID, ShedID, SubjectLabel string
	ProofRefs                                                      []string
	CapturedAt                                                     time.Time
	IdempotencyKey                                                 string
}

// DeathVerificationEnqueuer is the composition-layer seam to the verification module (the bridge in
// adapters/verificationbridge adapts verification's CreateItem; tasks never writes its tables).
type DeathVerificationEnqueuer interface {
	EnqueueDeathEvidenceVerification(ctx context.Context, in DeathVerificationEnqueueRequest) error
	EnqueueBirthEvidenceVerification(ctx context.Context, in BirthVerificationEnqueueRequest) error
}

// Service is the tasks app service.
type Service struct {
	repo     ports.Repository
	enqueuer DeathVerificationEnqueuer
	now      func() time.Time
	log      *slog.Logger
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
	if in.Module != domain.ModuleBirth && in.Module != domain.ModuleDeath {
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
	AnsweredBy         string
	IdempotencyKey     string
	RequestFingerprint string
}

// AnswerAction runs the answer write under the mandatory idempotency contract.
func (s *Service) AnswerAction(ctx context.Context, in AnswerActionInput) (domain.ActionWriteResult, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.WorkflowID) == "" ||
		strings.TrimSpace(in.ActionID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.ActionWriteResult{}, domain.ErrMissingRequiredField
	}
	result, err := s.repo.AnswerAction(ctx, domain.AnswerActionCommand{
		TenantID:           in.TenantID,
		WorkflowID:         in.WorkflowID,
		ActionID:           in.ActionID,
		AnswerValue:        strings.TrimSpace(in.AnswerValue),
		ProofRef:           strings.TrimSpace(in.ProofRef),
		AnsweredBy:         strings.TrimSpace(in.AnsweredBy),
		AnsweredAt:         s.now().UTC(),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.enqueueBirthWorkflowIfReady(ctx, in.TenantID, in.WorkflowID); err != nil {
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
	CompletedBy        string
	IdempotencyKey     string
	RequestFingerprint string
}

// deathEvidenceIdempotencyKey binds one verification item to the workflow AND the exact evidence
// pair being reviewed. Re-shot proofs after a rejection therefore open a NEW review item, while a
// retry of the same completion de-duplicates. See DeathVerificationEnqueueRequest.IdempotencyKey.
func deathEvidenceIdempotencyKey(workflowID string, round int, proofRefs []string) string {
	key := "counts-death-evidence:" + workflowID + ":r" + strconv.Itoa(round)
	for _, ref := range proofRefs {
		key += ":" + strings.TrimSpace(ref)
	}
	return key
}

func birthEvidenceIdempotencyKey(workflowID string, round int, proofRefs []string) string {
	key := "counts-birth-evidence:" + workflowID + ":r" + strconv.Itoa(round)
	for _, ref := range proofRefs {
		key += ":" + strings.TrimSpace(ref)
	}
	return key
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
	result, err := s.repo.CompleteAction(ctx, domain.CompleteActionCommand{
		TenantID:           in.TenantID,
		WorkflowID:         in.WorkflowID,
		ActionID:           in.ActionID,
		ProofRef:           strings.TrimSpace(in.ProofRef),
		CompletedBy:        strings.TrimSpace(in.CompletedBy),
		CompletedAt:        s.now().UTC(),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.ActionWriteResult{}, err
	}
	if err := s.enqueueBirthWorkflowIfReady(ctx, in.TenantID, in.WorkflowID); err != nil {
		return domain.ActionWriteResult{}, err
	}

	if result.NeedsVerificationEnqueue {
		if s.enqueuer == nil {
			// Composition bug, surfaced loudly. The completion is durable; wiring the enqueuer and
			// replaying the same request heals (state-derived NeedsVerificationEnqueue + idempotent
			// CreateItem).
			return domain.ActionWriteResult{}, domain.ErrVerificationEnqueuerNotWired
		}
		// Fetch shed details for operational location composition
		shedID := derefOr(result.Workflow.ShedID)
		shedName, partitionLabel, _ := s.repo.FetchShedDetails(ctx, in.TenantID, shedID)

		if err := s.enqueuer.EnqueueDeathEvidenceVerification(ctx, DeathVerificationEnqueueRequest{
			TenantID:       in.TenantID,
			WorkflowID:     in.WorkflowID,
			OperatorID:     strings.TrimSpace(in.CompletedBy),
			ParkID:         derefOr(result.Workflow.ParkID),
			ShedID:         shedID,
			ProofRefs:      result.DeathProofRefs,
			SubjectLabel:   appendLocation("Death evidence · "+result.Workflow.EventDate, shedName, partitionLabel),
			CapturedAt:     s.now().UTC(),
			IdempotencyKey: deathEvidenceIdempotencyKey(in.WorkflowID, result.DeathReviewRound, result.DeathProofRefs),
		}); err != nil {
			return domain.ActionWriteResult{}, err
		}
	}
	return result, nil
}

func (s *Service) enqueueBirthWorkflowIfReady(ctx context.Context, tenantID, workflowID string) error {
	review, err := s.repo.BirthWorkflowEvidenceForVerification(ctx, tenantID, workflowID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.enqueuer == nil {
		return domain.ErrVerificationEnqueuerNotWired
	}
	subject := "Child"
	if review.SubjectRole == domain.TemplateKeyBirthMother {
		subject = "Mother"
	}

	// Fetch shed details for operational location composition
	shedName, partitionLabel, _ := s.repo.FetchShedDetails(ctx, tenantID, review.ShedID)

	return s.enqueuer.EnqueueBirthEvidenceVerification(ctx, BirthVerificationEnqueueRequest{
		TenantID: tenantID, WorkflowID: review.WorkflowID, OperatorID: review.OperatorID,
		ParkID: review.ParkID, ShedID: review.ShedID, ProofRefs: review.ProofRefs,
		SubjectLabel:   appendLocation(subject+" birth evidence · "+review.EventDate, shedName, partitionLabel),
		CapturedAt:     s.now().UTC(),
		IdempotencyKey: birthEvidenceIdempotencyKey(review.WorkflowID, review.Round, review.ProofRefs),
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
}

// OpenDeathWorkflow opens the death workflow. Idempotent on the natural key.
func (s *Service) OpenDeathWorkflow(ctx context.Context, in OpenDeathWorkflowInput) error {
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = s.now()
	}
	_, err := s.repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:      in.TenantID,
		TemplateKey:   domain.TemplateKeyDeath,
		SubjectGoatID: in.GoatID,
		EventAt:       occurred,
		ParkID:        optionalUUID(in.ParkID),
		ShedID:        optionalUUID(in.ShedID),
	})
	return err
}

// OpenReportedDeathWorkflow reads the still-live goat's canonical placement and opens the two
// upload actions as soon as the death report is submitted, before any admin decision.
func (s *Service) OpenReportedDeathWorkflow(ctx context.Context, tenantID, goatID string, reportedAt time.Time) error {
	facts, err := s.repo.GoatWorkflowFacts(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	return s.OpenDeathWorkflow(ctx, OpenDeathWorkflowInput{
		TenantID: tenantID, GoatID: goatID,
		ParkID: derefOr(facts.ParkID), ShedID: derefOr(facts.ShedID), OccurredAt: reportedAt,
	})
}

// ReleaseApprovedDeathEvidence is called by the approved death's goat.exited event. The approval
// transaction has already proven both videos and moved the internal review action to in_review;
// this method performs the idempotent cross-module enqueue. Event redelivery heals a transient
// enqueue failure without duplicating the verifier item.
func (s *Service) ReleaseApprovedDeathEvidence(ctx context.Context, tenantID, goatID string, capturedAt time.Time) error {
	review, err := s.repo.DeathEvidenceForVerification(ctx, tenantID, goatID)
	if err != nil {
		return err
	}
	if s.enqueuer == nil {
		return domain.ErrVerificationEnqueuerNotWired
	}
	if capturedAt.IsZero() {
		capturedAt = s.now().UTC()
	}
	// Fetch shed details for operational location composition
	shedName, partitionLabel, _ := s.repo.FetchShedDetails(ctx, tenantID, review.ShedID)

	return s.enqueuer.EnqueueDeathEvidenceVerification(ctx, DeathVerificationEnqueueRequest{
		TenantID: tenantID, WorkflowID: review.WorkflowID, OperatorID: review.OperatorID,
		ParkID: review.ParkID, ShedID: review.ShedID, ProofRefs: review.ProofRefs,
		SubjectLabel: appendLocation("Death evidence · "+review.EventDate, shedName, partitionLabel), CapturedAt: capturedAt,
		IdempotencyKey: deathEvidenceIdempotencyKey(review.WorkflowID, review.Round, review.ProofRefs),
	})
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
