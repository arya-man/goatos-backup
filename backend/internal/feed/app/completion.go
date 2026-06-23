package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
)

// ObligationCompleter is the slice of the obligation repo SM-5 needs: flip the shed direction's
// obligation to completed (+ 'completed' event) on accept.
type ObligationCompleter interface {
	MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error)
}

// CompletionService implements the SM-5 verification + completion path for feed directions. Two
// flows, mirroring vaccination:
//   - Accept/Reject: record a direction AND verify it in one call.
//   - AcceptExisting/RejectExisting: verify a direction already recorded at SOP-submit time.
//
// An accepted direction completes its obligation; a rejected one leaves it open (rework). Stock
// consume is wired in a later slice (feed reserve/consume). Idempotent end-to-end (double-submit
// guard + accept-only-when-recorded). In-process today; same code runs behind a future Pub/Sub
// verification consumer.
type CompletionService struct {
	feed *Service
	obl  ObligationCompleter
}

// NewCompletionService wires the feed service and the obligation completer.
func NewCompletionService(feed *Service, obl ObligationCompleter) *CompletionService {
	return &CompletionService{feed: feed, obl: obl}
}

// AcceptInput carries the direction record + verification metadata.
type AcceptInput struct {
	Direction  domain.NewDirection
	VerifiedBy *string
}

// AcceptResult reports the outcome. Applied is false on a replay/double-submit no-op. Completed is
// true when this call transitioned the obligation to completed.
type AcceptResult struct {
	CompletionID string
	Applied      bool
	Completed    bool
}

// Accept records the direction, accepts it on verification, and completes its obligation. A
// double-submit (same tenant/obligation or idempotency key) is a no-op.
func (s *CompletionService) Accept(ctx context.Context, in AcceptInput) (AcceptResult, error) {
	in.Direction.Status = "recorded"
	cid, applied, err := s.feed.RecordDirection(ctx, in.Direction)
	if err != nil {
		return AcceptResult{}, err
	}
	if !applied {
		return AcceptResult{Applied: false}, nil // replay / double-submit
	}
	if _, _, err := s.feed.AcceptDirection(ctx, in.Direction.TenantID, cid, in.VerifiedBy); err != nil {
		return AcceptResult{}, err
	}
	completed, err := s.obl.MarkCompleted(ctx, in.Direction.TenantID, in.Direction.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{CompletionID: cid, Applied: true, Completed: completed}, nil
}

// AcceptExisting accepts an already-recorded direction (the SOP verify outcome): it flips the
// direction to accepted and completes its obligation. Idempotent: a direction no longer in
// 'recorded' state is a no-op.
func (s *CompletionService) AcceptExisting(ctx context.Context, tenantID, completionID string, verifiedBy *string) (AcceptResult, error) {
	acc, applied, err := s.feed.AcceptDirection(ctx, tenantID, completionID, verifiedBy)
	if err != nil {
		return AcceptResult{}, err
	}
	if !applied {
		return AcceptResult{Applied: false}, nil // already verified
	}
	completed, err := s.obl.MarkCompleted(ctx, tenantID, acc.ObligationID)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{CompletionID: completionID, Applied: true, Completed: completed}, nil
}

// RejectInput carries the direction record + the rejection reason.
type RejectInput struct {
	Direction  domain.NewDirection
	Reason     string
	VerifiedBy *string
}

// RejectResult reports the outcome. Applied is false on a replay no-op.
type RejectResult struct {
	CompletionID string
	Applied      bool
}

// Reject records the direction then rejects it (rework): the obligation stays open. Idempotent.
func (s *CompletionService) Reject(ctx context.Context, in RejectInput) (RejectResult, error) {
	in.Direction.Status = "recorded"
	cid, applied, err := s.feed.RecordDirection(ctx, in.Direction)
	if err != nil {
		return RejectResult{}, err
	}
	if !applied {
		return RejectResult{Applied: false}, nil
	}
	if _, err := s.feed.RejectDirection(ctx, in.Direction.TenantID, cid, in.Reason, in.VerifiedBy); err != nil {
		return RejectResult{}, err
	}
	return RejectResult{CompletionID: cid, Applied: true}, nil
}

// RejectExisting rejects an already-recorded direction (the SOP rework outcome). The obligation
// stays open. Idempotent: a direction no longer in 'recorded' state is a no-op.
func (s *CompletionService) RejectExisting(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (RejectResult, error) {
	applied, err := s.feed.RejectDirection(ctx, tenantID, completionID, reason, verifiedBy)
	if err != nil {
		return RejectResult{}, err
	}
	return RejectResult{CompletionID: completionID, Applied: applied}, nil
}
