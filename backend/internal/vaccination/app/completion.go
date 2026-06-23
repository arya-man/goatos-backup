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
type StockConsumer interface {
	ConsumeForBatch(ctx context.Context, tenantID, batchID, lotID, itemID, locationID, key string, qty int64) error
}

// CompletionService implements SM-5 verification + completion: record a dose, accept/reject it,
// complete the obligation on accept, and consume the goat's reserved dose from the drive lot.
// Idempotent end-to-end — the completion double-submit guard (UNIQUE tenant,obligation,goat) plus
// the movement idempotency key make a replay a no-op. In-process today; the same code runs behind a
// future Pub/Sub verification consumer.
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

// AcceptInput carries the dose record (Completion), the verification metadata, and the consume
// context. Stock is consumed only when the completion has a BatchID + VaccineInventoryLotID and
// ItemID/LocationID are set; otherwise consume is skipped (best-effort).
type AcceptInput struct {
	Completion      domain.NewCompletion
	VerifiedBy      *string
	WithdrawalUntil *time.Time
	ItemID          string
	LocationID      string
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
	if err := s.vacc.AcceptCompletion(ctx, in.Completion.TenantID, cid, in.VerifiedBy, in.WithdrawalUntil); err != nil {
		return AcceptResult{}, err
	}
	completed, err := s.obl.MarkCompleted(ctx, in.Completion.TenantID, in.Completion.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	if s.inv != nil && in.Completion.BatchID != nil && in.Completion.VaccineInventoryLotID != nil &&
		in.ItemID != "" && in.LocationID != "" {
		doses := int64(1)
		if in.Completion.Doses != nil && *in.Completion.Doses > 0 {
			doses = int64(*in.Completion.Doses)
		}
		key := *in.Completion.BatchID + ":consume:" + in.Completion.GoatID
		if err := s.inv.ConsumeForBatch(ctx, in.Completion.TenantID, *in.Completion.BatchID,
			*in.Completion.VaccineInventoryLotID, in.ItemID, in.LocationID, key, doses); err != nil {
			return AcceptResult{}, err
		}
	}
	nextScheduled := false
	if s.booster != nil && in.ProtocolVersionID != "" {
		nextScheduled, err = s.booster.ScheduleNextDose(ctx, ScheduleNextInput{
			TenantID:          in.Completion.TenantID,
			ProtocolVersionID: in.ProtocolVersionID,
			GoatID:            in.Completion.GoatID,
			ScopeType:         in.ScopeType,
			ScopeID:           in.ScopeID,
			PrevSequence:      in.RuleSequence,
			AdministeredAt:    in.Completion.AdministeredAt,
		})
		if err != nil {
			return AcceptResult{}, err
		}
	}
	return AcceptResult{CompletionID: cid, Applied: true, Completed: completed, NextScheduled: nextScheduled}, nil
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
	if err := s.vacc.RejectCompletion(ctx, in.Completion.TenantID, cid, in.Reason, in.VerifiedBy); err != nil {
		return RejectResult{}, err
	}
	return RejectResult{CompletionID: cid, Applied: true}, nil
}
