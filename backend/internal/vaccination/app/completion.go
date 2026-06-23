package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ObligationCompleter is the slice of the obligation repo SM-5 needs: flip the obligation to
// completed (+ 'completed' event) when its verification is accepted.
type ObligationCompleter interface {
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)
}

// StockConsumer is the slice of the inventory service SM-5 needs: consume reserved doses on accept.
// The lot's item/location/unit are derived from the lot itself, so callers pass only (batch, lot).
type StockConsumer interface {
	ConsumeForBatch(ctx context.Context, tenantID, batchID, lotID, key string, qty int64) error
}

// CompletionService implements SM-5 verification + completion. It supports two flows:
//   - Accept/Reject: record a dose AND verify it in one call (programmatic / direct use).
//   - AcceptExisting/RejectExisting: verify a completion already recorded at SOP-submit time (the
//     two-phase operator flow — record at submit, verify at review).
//
// Either way an accepted dose completes its obligation and consumes the reserved dose; a rejected
// dose leaves the obligation open (rework). Idempotent end-to-end (double-submit guard + accept-only-
// when-recorded + keyed movements). In-process today; the same code runs behind a future Pub/Sub
// verification consumer.
type CompletionService struct {
	vacc    *Service
	obl     ObligationCompleter
	inv     StockConsumer
	booster *BoosterService
}

// NewCompletionService wires the vaccination service, the obligation completer, and the stock
// consumer. inv may be nil to disable stock consumption (e.g. non-drive flows).
func NewCompletionService(vacc *Service, obl ObligationCompleter, inv StockConsumer) *CompletionService {
	return &CompletionService{vacc: vacc, obl: obl, inv: inv}
}

// WithBooster enables SM-7: on accept, schedule the next after_previous_completion dose from the
// administered date. Returns the service for chaining.
func (s *CompletionService) WithBooster(b *BoosterService) *CompletionService {
	s.booster = b
	return s
}

// AcceptInput carries the dose record (Completion), the verification metadata, and the optional SM-7
// booster context. The consume context (item/location/unit) is derived from the completion's lot.
type AcceptInput struct {
	Completion      domain.NewCompletion
	VerifiedBy      *string
	WithdrawalUntil *time.Time
	// SM-7 booster context (used only when a booster is wired): the protocol version + the
	// administered dose's scope and sequence, so the next dose can be scheduled.
	ProtocolVersionID string
	ScopeType         string
	ScopeID           string
	RuleSequence      int32
}

// AcceptResult reports the outcome. Applied is false on a replay/double-submit no-op. Completed is
// true when this call transitioned the obligation to completed. NextScheduled is true when a booster
// dose was scheduled (SM-7).
type AcceptResult struct {
	CompletionID  string
	Applied       bool
	Completed     bool
	NextScheduled bool
}

// Accept records the dose, accepts it on verification, completes the obligation, and consumes the
// reserved dose. A double-submit (same tenant/obligation/goat or idempotency key) is a no-op.
func (s *CompletionService) Accept(ctx context.Context, in AcceptInput) (AcceptResult, error) {
	in.Completion.Status = "recorded"
	cid, applied, err := s.vacc.RecordCompletion(ctx, in.Completion)
	if err != nil {
		return AcceptResult{}, err
	}
	if !applied {
		return AcceptResult{Applied: false}, nil // replay / double-submit
	}
	if _, _, err := s.vacc.AcceptCompletion(ctx, in.Completion.TenantID, cid, in.VerifiedBy, in.WithdrawalUntil); err != nil {
		return AcceptResult{}, err
	}
	completed, err := s.obl.MarkCompleted(ctx, in.Completion.TenantID, in.Completion.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	if err := s.consume(ctx, in.Completion.TenantID, deref(in.Completion.BatchID),
		deref(in.Completion.VaccineInventoryLotID), in.Completion.GoatID, in.Completion.Doses); err != nil {
		return AcceptResult{}, err
	}
	next, err := s.scheduleBooster(ctx, in.Completion.TenantID, in.ProtocolVersionID, in.Completion.GoatID,
		in.ScopeType, in.ScopeID, in.RuleSequence, in.Completion.AdministeredAt)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{CompletionID: cid, Applied: true, Completed: completed, NextScheduled: next}, nil
}

// AcceptExistingInput verifies a completion already recorded (at SOP-submit time) by its id, plus the
// optional SM-7 booster context (the protocol version + the dose's scope and sequence).
type AcceptExistingInput struct {
	TenantID          string
	CompletionID      string
	VerifiedBy        *string
	WithdrawalUntil   *time.Time
	ProtocolVersionID string
	ScopeType         string
	ScopeID           string
	RuleSequence      int32
}

// AcceptExisting accepts an already-recorded completion (the SOP verify outcome): it flips the
// completion to accepted, completes its obligation, consumes the reserved dose, and schedules the
// next booster. Idempotent: a completion no longer in 'recorded' state is a no-op.
func (s *CompletionService) AcceptExisting(ctx context.Context, in AcceptExistingInput) (AcceptResult, error) {
	acc, applied, err := s.vacc.AcceptCompletion(ctx, in.TenantID, in.CompletionID, in.VerifiedBy, in.WithdrawalUntil)
	if err != nil {
		return AcceptResult{}, err
	}
	if !applied {
		return AcceptResult{Applied: false}, nil // already verified
	}
	completed, err := s.obl.MarkCompleted(ctx, in.TenantID, acc.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	if err := s.consume(ctx, in.TenantID, acc.BatchID, acc.LotID, acc.GoatID, &acc.Doses); err != nil {
		return AcceptResult{}, err
	}
	next, err := s.scheduleBooster(ctx, in.TenantID, in.ProtocolVersionID, acc.GoatID,
		in.ScopeType, in.ScopeID, in.RuleSequence, acc.AdministeredAt)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{CompletionID: in.CompletionID, Applied: true, Completed: completed, NextScheduled: next}, nil
}

// RejectInput carries the dose record + the rejection reason. No obligation completion, no consume.
type RejectInput struct {
	Completion domain.NewCompletion
	Reason     string
	VerifiedBy *string
}

// RejectResult reports the outcome. Applied is false on a replay no-op.
type RejectResult struct {
	CompletionID string
	Applied      bool
}

// Reject records the dose then rejects it (rework): the obligation stays open and no stock is
// consumed. Idempotent — a replay is a no-op.
func (s *CompletionService) Reject(ctx context.Context, in RejectInput) (RejectResult, error) {
	in.Completion.Status = "recorded"
	cid, applied, err := s.vacc.RecordCompletion(ctx, in.Completion)
	if err != nil {
		return RejectResult{}, err
	}
	if !applied {
		return RejectResult{Applied: false}, nil
	}
	if _, err := s.vacc.RejectCompletion(ctx, in.Completion.TenantID, cid, in.Reason, in.VerifiedBy); err != nil {
		return RejectResult{}, err
	}
	return RejectResult{CompletionID: cid, Applied: true}, nil
}

// RejectExisting rejects an already-recorded completion (the SOP rework outcome). The obligation
// stays open. Idempotent: a completion no longer in 'recorded' state is a no-op.
func (s *CompletionService) RejectExisting(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (RejectResult, error) {
	applied, err := s.vacc.RejectCompletion(ctx, tenantID, completionID, reason, verifiedBy)
	if err != nil {
		return RejectResult{}, err
	}
	return RejectResult{CompletionID: completionID, Applied: applied}, nil
}

// consume consumes the goat's reserved dose for a drive batch. No-op when stock is not wired, the
// completion was not part of a drive (no batch/lot), or doses is zero.
func (s *CompletionService) consume(ctx context.Context, tenantID, batchID, lotID, goatID string, doses *int32) error {
	if s.inv == nil || batchID == "" || lotID == "" {
		return nil
	}
	q := int64(1)
	if doses != nil && *doses > 0 {
		q = int64(*doses)
	}
	return s.inv.ConsumeForBatch(ctx, tenantID, batchID, lotID, batchID+":consume:"+goatID, q)
}

// scheduleBooster runs SM-7 when a booster is wired and a protocol version is given.
func (s *CompletionService) scheduleBooster(ctx context.Context, tenantID, versionID, goatID, scopeType, scopeID string, prevSeq int32, administered time.Time) (bool, error) {
	if s.booster == nil || versionID == "" {
		return false, nil
	}
	return s.booster.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		GoatID:            goatID,
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		PrevSequence:      prevSeq,
		AdministeredAt:    administered,
	})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
