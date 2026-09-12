// Package verificationbridge adapts the verification module's CreateItem to the pen-visit
// VerificationEnqueuer port, so a submitted visit (ONE clip) becomes ONE generic verification
// item the verifier queue lists. penvisits never writes verification's tables.
package verificationbridge

import (
	"context"
	"strings"

	penvisitsapp "github.com/vgoats/goatos/backend/internal/penvisits/app"
	penvisitsdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the ONE verification-service method this bridge consumes.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// Enqueuer bridges pen-visit submits into the verifier queue.
type Enqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ penvisitsapp.VerificationEnqueuer = (*Enqueuer)(nil)

// EnqueuePenVisitVerification maps the submit to a verification CreateItem. CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates; a re-shoot after a rework carries a new row version and mints a fresh item.
func (e *Enqueuer) EnqueuePenVisitVerification(ctx context.Context, in penvisitsapp.VerificationEnqueueRequest) error {
	// Subject: "Pen visit · Castro 2" -- degrades to the bare label rather than composing a
	// dangling separator.
	label := "Pen visit"
	if pen := strings.TrimSpace(in.PenLabel); pen != "" {
		label = label + " · " + pen
	}
	rows := make([]verificationdomain.ContextRow, 0, 3)
	reasons := make([]string, 0, len(in.Reasons))
	for _, r := range penvisitsdomain.SortReasons(in.Reasons) {
		reasons = append(reasons, penvisitsdomain.ReasonLabel(r))
	}
	if len(reasons) > 0 {
		rows = append(rows, verificationdomain.ContextRow{Label: "Visit after", Value: strings.Join(reasons, ", ")})
	}
	if in.SourceDate != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Work done on", Value: biztime.FarmDateFromBusinessDate(in.SourceDate)})
	}
	if park := strings.TrimSpace(in.ParkName); park != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Park", Value: park})
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:       in.TenantID,
		Vertical:       penvisitsapp.VerificationVertical,
		Module:         penvisitsapp.VerificationModule,
		Category:       penvisitsapp.VerificationCategory,
		SubjectLabel:   &label,
		ContextRows:    rows,
		Source:         verificationdomain.SourceRef{Module: penvisitsapp.VerificationModule, RefType: penvisitsapp.VerificationRefType, RefID: in.TaskID},
		MediaRefs:      []string{in.ProofRef},
		OperatorID:     ptrIfSet(in.SubmittedBy),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

func ptrIfSet(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
