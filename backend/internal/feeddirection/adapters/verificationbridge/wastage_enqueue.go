package verificationbridge

import (
	"context"
	"strings"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// wastageVerificationService is the slice of the verification service the wastage bridge needs:
// enqueue one item, and restate one item's subject label after the verifier records a measurement.
// A separate, wider slice than verificationCreator so the distribution/packing bridges keep
// compiling against exactly what they use.
type wastageVerificationService interface {
	verificationCreator
	RelabelItemBySource(ctx context.Context, tenantID, sourceModule, sourceRefType, sourceRefID, subjectLabel string) (int, error)
}

// WastageEnqueuer adapts the verification module's CreateItem to the feeddirection
// FeedWastageVerificationEnqueuer port, and carries the relabel seam the measurement service uses
// to keep the queue row in step with the recorded value. Feeddirection never writes verification's
// tables.
type WastageEnqueuer struct {
	verification wastageVerificationService
}

// NewWastage constructs the wastage bridge over the verification service.
func NewWastage(v wastageVerificationService) *WastageEnqueuer {
	return &WastageEnqueuer{verification: v}
}

var _ feeddirectionapp.FeedWastageVerificationEnqueuer = (*WastageEnqueuer)(nil)
var _ feeddirectionapp.WastageVerificationRelabeler = (*WastageEnqueuer)(nil)

// EnqueueFeedWastageVerification maps the feeddirection request to a verification CreateItem. The
// single wastage video travels on ONE item. CreateItem is idempotent on (tenant, idempotency_key),
// so a retry after a prior failure heals rather than duplicates.
func (e *WastageEnqueuer) EnqueueFeedWastageVerification(ctx context.Context, in feeddirectionapp.FeedWastageVerificationEnqueueRequest) error {
	// The subject names the PEN — "Castro - 2". No session prefix: a pen owes exactly one wastage
	// video per feed day, so the pen alone identifies the card. Once the verifier records a
	// measurement, the measurement service restates this label with the value.
	//
	// Degrades rather than composing a dangling separator: an unresolvable location leaves the
	// label nil and the item falls back to the verification module's own defaults.
	loc := oploc.OperationalLocation{ShedName: in.ShedName, PartitionLabel: in.PartitionLabel}
	locDisplay := loc.Display()
	var label *string
	if locDisplay != "" {
		label = &locDisplay
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: in.TenantID,
		Vertical: feeddirectiondomain.VerificationVerticalFeed,
		Module:   feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryWastage,
		// Backend owns this display string (dumb-renderer rule).
		SubjectLabel: label,
		Source: verificationdomain.SourceRef{
			Module:  feeddirectiondomain.VerificationModuleFeed,
			RefType: feeddirectiondomain.VerificationRefTypeWastage,
			RefID:   in.CompletionID,
		},
		// What the verifier is judging the video AGAINST: which trial the pen is on and how many
		// animals it holds. Composed by the producer and rendered verbatim; a row is emitted only
		// when its value is known — never a placeholder, which would read as "no trial" rather
		// than "not known".
		ContextRows: wastageContextRows(in.ExperimentArm, in.HeadCountSummary),
		// One media ref: the wastage video.
		MediaRefs:  []string{in.WastageProofRef},
		OperatorID: ptrIfSet(in.OperatorID),
		ShedID:     ptrIfSet(in.ShedID),
		// The PEN as its own field, not only folded into the label, so filtering/grouping by pen
		// sees wastage items too (the packing bridge's 2026-08 lesson).
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// RelabelFeedWastageVerification restates the verification item's subject label after the verifier
// records the measured leftover, so the queue row shows the value instead of the bare pen. RefID is
// the completion id — the same id EnqueueFeedWastageVerification put in Source.RefID.
func (e *WastageEnqueuer) RelabelFeedWastageVerification(ctx context.Context, tenantID, completionID, subjectLabel string) error {
	if completionID == "" || subjectLabel == "" {
		return nil
	}
	_, err := e.verification.RelabelItemBySource(
		ctx,
		tenantID,
		feeddirectiondomain.VerificationModuleFeed,
		feeddirectiondomain.VerificationRefTypeWastage,
		completionID,
		subjectLabel,
	)
	return err
}

// wastageContextRows is the verifier's "what pen is this" block for a wastage proof. Labels are
// farm language, composed here because the backend owns visible copy.
func wastageContextRows(experimentArm, headCountSummary string) []verificationdomain.ContextRow {
	rows := make([]verificationdomain.ContextRow, 0, 2)
	if strings.TrimSpace(experimentArm) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Trial group", Value: experimentArm})
	}
	if strings.TrimSpace(headCountSummary) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Animals in this pen", Value: headCountSummary})
	}
	return rows
}
