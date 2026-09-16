// Package countsbridge holds composition-layer adapters that connect the counts module to other
// modules' service APIs without either module importing the other's storage. This mirrors sopbridge.
package countsbridge

import (
	"context"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// ShiftingVerificationEnqueuer adapts the verification module's CreateItem to the counts
// ShiftingVerificationEnqueuer port, so a completed shed move (mandatory video) becomes one generic
// verification item the verifier queue lists. Counts never writes verification's tables.
type ShiftingVerificationEnqueuer struct {
	verification verificationCreator
}

// NewShiftingVerificationEnqueuer constructs the bridge over the verification service.
func NewShiftingVerificationEnqueuer(v verificationCreator) *ShiftingVerificationEnqueuer {
	return &ShiftingVerificationEnqueuer{verification: v}
}

var _ countsapp.ShiftingVerificationEnqueuer = (*ShiftingVerificationEnqueuer)(nil)

// EnqueueShiftingMoveVerification maps the counts request to a verification CreateItem. CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates.
func (e *ShiftingVerificationEnqueuer) EnqueueShiftingMoveVerification(ctx context.Context, in countsapp.ShiftingVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     countsdomain.VerificationVerticalShifting,
		Module:       countsdomain.VerificationModuleShifting,
		Category:     countsdomain.VerificationCategoryShifting,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		SubjectNote:  ptrIfSet(in.SubjectNote),
		Source: verificationdomain.SourceRef{
			Module:  countsdomain.VerificationModuleShifting,
			RefType: countsdomain.VerificationRefTypeShifting,
			RefID:   in.ShiftingEventID,
		},
		ContextRows:    shiftingContextRows(in.ContextRows),
		MediaRefs:      in.MediaRefs,
		MediaMeta:      shiftingMediaMeta(in.MediaMeta),
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// shiftingContextRows translates the counts-side rows without reinterpreting them: counts composed
// the farm wording, this only crosses the module boundary. Order is preserved because the verifier
// reads "Moved from" above "Moved to", which is the direction the animals walked.
func shiftingContextRows(rows []countsapp.VerificationContextRow) []verificationdomain.ContextRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]verificationdomain.ContextRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, verificationdomain.ContextRow{Label: row.Label, Value: row.Value, Group: row.Group})
	}
	return out
}

// shiftingMediaMeta maps counts' per-proof {label, kind} onto verification's MediaMeta through the
// ONE shared builder (positional against MediaRefs, titles trimmed, kinds normalized). None means
// the item names none of its proofs and the queue falls back to the registry's positional copy.
func shiftingMediaMeta(meta []countsports.ProofMeta) []verificationdomain.MediaMeta {
	if len(meta) == 0 {
		return nil
	}
	captures := make([]verificationdomain.ProofCapture, 0, len(meta))
	for _, m := range meta {
		captures = append(captures, verificationdomain.ProofCapture{Title: m.Label, Kind: m.Kind})
	}
	return verificationdomain.BuildMediaMeta(captures)
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
