// Package app holds the vaccination application service. SM-5 verification, inventory consume,
// obligation completion, durable completion outbox, and completed-event booster handling wire through
// this service/repository boundary.
package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// Service coordinates vaccination use-cases over the repository boundary.
type Service struct {
	repo ports.Repository
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// RecordCompletion records an administered dose (idempotent).
func (s *Service) RecordCompletion(ctx context.Context, in domain.NewCompletion) (string, bool, error) {
	return s.repo.RecordCompletion(ctx, in)
}

// AcceptCompletion accepts a recorded completion on verification, returning its verification context
// (idempotent: applied is false on replay).
func (s *Service) AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptedCompletion, bool, error) {
	return s.repo.AcceptCompletion(ctx, tenantID, completionID, verifiedBy, withdrawalUntil)
}

type atomicCompletionRepository interface {
	AcceptCompletionAtomic(ctx context.Context, in domain.AcceptCompletionAtomicInput) (domain.AcceptCompletionAtomicResult, error)
}

type atomicRecordCompletionRepository interface {
	RecordAndAcceptCompletionAtomic(ctx context.Context, completion domain.NewCompletion, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptCompletionAtomicResult, error)
}

type acceptedCompletionByObligationRepository interface {
	GetAcceptedCompletionForObligation(ctx context.Context, tenantID, obligationID string) (domain.AcceptedCompletion, bool, error)
}

// AcceptCompletionAtomic accepts and completes an existing recorded completion through a repository
// transaction when the adapter supports it. The boolean reports whether the stronger path was used.
func (s *Service) AcceptCompletionAtomic(ctx context.Context, in domain.AcceptCompletionAtomicInput) (domain.AcceptCompletionAtomicResult, bool, error) {
	repo, ok := s.repo.(atomicCompletionRepository)
	if !ok {
		return domain.AcceptCompletionAtomicResult{}, false, nil
	}
	result, err := repo.AcceptCompletionAtomic(ctx, in)
	return result, true, err
}

// RecordAndAcceptCompletionAtomic records a direct completion and accepts it in one repository
// transaction when the adapter supports it. The boolean reports whether the stronger path was used.
func (s *Service) RecordAndAcceptCompletionAtomic(ctx context.Context, completion domain.NewCompletion, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptCompletionAtomicResult, bool, error) {
	repo, ok := s.repo.(atomicRecordCompletionRepository)
	if !ok {
		return domain.AcceptCompletionAtomicResult{}, false, nil
	}
	result, err := repo.RecordAndAcceptCompletionAtomic(ctx, completion, verifiedBy, withdrawalUntil)
	return result, true, err
}

// GetRecordedCompletion returns the stock/obligation context for a completion still awaiting
// verification. It lets SM-5 consume stock before flipping the completion accepted.
func (s *Service) GetRecordedCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	return s.repo.GetRecordedCompletion(ctx, tenantID, completionID)
}

// GetAcceptableCompletion returns the verification context for a completion that is either still
// recorded or already accepted. It is used only for retry/resume of idempotent accept side effects.
func (s *Service) GetAcceptableCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	return s.repo.GetAcceptableCompletion(ctx, tenantID, completionID)
}

// GetAcceptableCompletionByIdempotency returns the existing direct-accept completion for a replayed
// idempotency key so later side effects can be resumed.
func (s *Service) GetAcceptableCompletionByIdempotency(ctx context.Context, tenantID, idempotencyKey string) (domain.AcceptedCompletion, bool, error) {
	return s.repo.GetAcceptableCompletionByIdempotency(ctx, tenantID, idempotencyKey)
}

// GetAcceptedCompletionForObligation returns the accepted dose that completed an obligation when the
// repository supports the lookup. It is used by the vaccination.completed consumer.
func (s *Service) GetAcceptedCompletionForObligation(ctx context.Context, tenantID, obligationID string) (domain.AcceptedCompletion, bool, error) {
	repo, ok := s.repo.(acceptedCompletionByObligationRepository)
	if !ok {
		return domain.AcceptedCompletion{}, false, ports.ErrNotFound
	}
	return repo.GetAcceptedCompletionForObligation(ctx, tenantID, obligationID)
}

// RejectCompletion rejects a recorded completion (rework). applied is false on replay.
func (s *Service) RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	return s.repo.RejectCompletion(ctx, tenantID, completionID, reason, verifiedBy)
}

// GoatHistory returns a goat's vaccination history.
func (s *Service) GoatHistory(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error) {
	return s.repo.ListCompletionsByGoat(ctx, tenantID, goatID, limit)
}

// RecordedCompletionsByTask returns the still-recorded completion ids under a SOP task (verify
// fan-out source).
func (s *Service) RecordedCompletionsByTask(ctx context.Context, tenantID, taskID string) ([]string, error) {
	return s.repo.ListRecordedCompletionsByTask(ctx, tenantID, taskID)
}

// RecordCompletionsFromSubmission creates one recorded completion per goat item in a vaccination
// SOP submission. This is the submit-time bridge before verification accepts/rejects the proof.
func (s *Service) RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error) {
	return s.repo.RecordCompletionsFromSubmission(ctx, tenantID, taskID, submissionID, recordedBy)
}

// SubmissionCompletions returns the per-goat completion context created by one SOP submission.
func (s *Service) SubmissionCompletions(ctx context.Context, tenantID, submissionID string) ([]domain.SubmissionCompletion, error) {
	return s.repo.ListSubmissionCompletions(ctx, tenantID, submissionID)
}

// ShedCompletionSummary returns the read-only shed-completion/submit summary for a vaccination
// task: shed completion is an acknowledgement, not a manual form, so the summary is computed
// entirely from scan + proof + obligation state, never from submitted answers.
func (s *Service) ShedCompletionSummary(ctx context.Context, tenantID, taskID, shedID string, partitionLabel ...string) (domain.ShedCompletionSummary, error) {
	return s.repo.ShedCompletionSummary(ctx, tenantID, taskID, shedID, partitionLabel...)
}

// LastAccepted returns a goat's most recent accepted administration (Goat Passport / SM-7 basis).
func (s *Service) LastAccepted(ctx context.Context, tenantID, goatID string) (domain.LastAccepted, bool, error) {
	return s.repo.GetLastAcceptedForGoat(ctx, tenantID, goatID)
}

// VerificationQueue returns completions awaiting review (status='recorded'), earliest administered
// first.
func (s *Service) VerificationQueue(ctx context.Context, tenantID, parkID string, cursor *domain.RecordedCompletionCursor, limit int32) (domain.RecordedCompletionPage, error) {
	return s.repo.ListRecordedCompletions(ctx, tenantID, parkID, cursor, limit)
}

// RecomputeEligibilityRollup fully rebuilds the tenant's vaccination eligibility rollup read model from
// the source tables. This is the projector/CLI entry point — a heavy full-herd aggregate that must run
// off the UI request path, never from a preview handler.
func (s *Service) RecomputeEligibilityRollup(ctx context.Context, tenantID string) (domain.RollupRecomputeResult, error) {
	return s.repo.RecomputeEligibilityRollup(ctx, tenantID)
}
