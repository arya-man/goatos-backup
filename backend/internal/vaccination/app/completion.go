package app

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

var ErrStockGateBlocked = errors.New("vaccination: stock gate blocked")

// ObligationCompleter is the slice of the obligation repo SM-5 needs: flip the obligation to
// completed (+ 'completed' event) on accept, and (for SM-7 on the verify path) read its protocol
// version + scope + sequence so the next dose can be scheduled without the caller threading that
// context through the verification event.
type ObligationCompleter interface {
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)
	GetBoosterContext(ctx context.Context, tenantID, obligationID string) (versionID, scopeType, scopeID string, sequence int32, err error)
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
	if err := s.validateStockGate(in.Completion); err != nil {
		return AcceptResult{}, err
	}
	in.Completion.Status = "recorded"
	cid, applied, err := s.vacc.RecordCompletion(ctx, in.Completion)
	if err != nil {
		return AcceptResult{}, err
	}
	pending := domain.AcceptedCompletion{CompletionID: cid, Status: "recorded", ObligationID: in.Completion.ObligationID, GoatID: in.Completion.GoatID, BatchID: deref(in.Completion.BatchID), LotID: deref(in.Completion.VaccineInventoryLotID), Doses: derefDose(in.Completion.Doses), AdministeredAt: in.Completion.AdministeredAt}
	if !applied {
		found := false
		pending, found, err = s.vacc.GetAcceptableCompletionByIdempotency(ctx, in.Completion.TenantID, in.Completion.IdempotencyKey)
		if err != nil {
			return AcceptResult{}, err
		}
		if !found {
			return AcceptResult{Applied: false}, nil // rejected/reversed replay or unknown key
		}
	}
	acceptApplied := false
	if pending.Status == "recorded" {
		acc, accepted, err := s.vacc.AcceptCompletion(ctx, in.Completion.TenantID, pending.CompletionID, in.VerifiedBy, in.WithdrawalUntil)
		if err != nil {
			return AcceptResult{}, err
		}
		if !accepted {
			return AcceptResult{CompletionID: pending.CompletionID, Applied: false}, nil
		}
		pending = acc
		acceptApplied = true
	}
	if pending.Status != "accepted" {
		return AcceptResult{CompletionID: pending.CompletionID, Applied: false}, nil
	}
	if err := s.consume(ctx, in.Completion.TenantID, pending.BatchID, pending.LotID, pending.ObligationID, pending.GoatID, &pending.Doses); err != nil {
		return AcceptResult{}, err
	}
	completed, err := s.obl.MarkCompleted(ctx, in.Completion.TenantID, pending.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	next, err := s.scheduleBooster(ctx, in.Completion.TenantID, in.ProtocolVersionID, pending.GoatID,
		in.ScopeType, in.ScopeID, in.RuleSequence, pending.AdministeredAt)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{CompletionID: pending.CompletionID, Applied: applied || acceptApplied || completed || next, Completed: completed, NextScheduled: next}, nil
}

// AcceptExistingInput verifies a completion already recorded (at SOP-submit time) by its id. The
// SM-7 booster context is derived from the obligation, so the verification event need only carry the
// completion id.
type AcceptExistingInput struct {
	TenantID        string
	CompletionID    string
	VerifiedBy      *string
	WithdrawalUntil *time.Time
}

// AcceptExisting accepts an already-recorded completion (the SOP verify outcome): it flips the
// completion to accepted, completes its obligation, consumes the reserved dose, and schedules the
// next booster (deriving the protocol version + scope + sequence from the obligation). Idempotent: a
// completion no longer in 'recorded' state is a no-op.
func (s *CompletionService) AcceptExisting(ctx context.Context, in AcceptExistingInput) (AcceptResult, error) {
	pending, found, err := s.vacc.GetAcceptableCompletion(ctx, in.TenantID, in.CompletionID)
	if err != nil {
		return AcceptResult{}, err
	}
	if !found {
		return AcceptResult{Applied: false}, nil
	}
	applied := false
	if pending.Status == "recorded" {
		acc, accepted, err := s.vacc.AcceptCompletion(ctx, in.TenantID, in.CompletionID, in.VerifiedBy, in.WithdrawalUntil)
		if err != nil {
			return AcceptResult{}, err
		}
		if !accepted {
			return AcceptResult{CompletionID: in.CompletionID, Applied: false}, nil
		}
		pending = acc
		applied = true
	}
	if pending.Status != "accepted" {
		return AcceptResult{CompletionID: in.CompletionID, Applied: false}, nil
	}
	if err := s.consume(ctx, in.TenantID, pending.BatchID, pending.LotID, pending.ObligationID, pending.GoatID, &pending.Doses); err != nil {
		return AcceptResult{}, err
	}
	completed, err := s.obl.MarkCompleted(ctx, in.TenantID, pending.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	next := false
	if s.booster != nil {
		versionID, scopeType, scopeID, seq, gerr := s.obl.GetBoosterContext(ctx, in.TenantID, pending.ObligationID)
		if gerr != nil {
			return AcceptResult{}, gerr
		}
		if next, err = s.scheduleBooster(ctx, in.TenantID, versionID, pending.GoatID, scopeType, scopeID, seq, pending.AdministeredAt); err != nil {
			return AcceptResult{}, err
		}
	}
	return AcceptResult{CompletionID: in.CompletionID, Applied: applied || completed || next, Completed: completed, NextScheduled: next}, nil
}

func (s *CompletionService) validateStockGate(in domain.NewCompletion) error {
	if s.inv == nil || in.BatchID == nil || *in.BatchID == "" {
		return nil
	}
	if in.VaccineInventoryLotID == nil || *in.VaccineInventoryLotID == "" {
		return ErrStockGateBlocked
	}
	if !in.ColdChainVerified {
		return ErrStockGateBlocked
	}
	return nil
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
func (s *CompletionService) consume(ctx context.Context, tenantID, batchID, lotID, obligationID, goatID string, doses *int32) error {
	if s.inv == nil || batchID == "" || lotID == "" {
		return nil
	}
	q := int64(1)
	if doses != nil && *doses > 0 {
		q = int64(*doses)
	}
	return s.inv.ConsumeForBatch(ctx, tenantID, batchID, lotID, batchID+":consume:"+obligationID+":"+goatID, q)
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

func derefDose(p *int32) int32 {
	if p == nil || *p <= 0 {
		return 1
	}
	return *p
}
