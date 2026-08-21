package verificationbridge

import (
	"context"
	"fmt"
	"strings"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// PackingEnqueuer adapts the verification module's CreateItem to the feeddirection
// FeedPackingVerificationEnqueuer port, so a completed feed packing session (one mandatory video)
// becomes one generic verification item the verifier queue lists. It reuses the same verificationCreator
// slice as the distribution Enqueuer; only the category/ref_type and single media ref differ.
// Feeddirection never writes verification's tables.
type PackingEnqueuer struct {
	verification verificationCreator
}

// NewPacking constructs the packing bridge over the verification service.
func NewPacking(v verificationCreator) *PackingEnqueuer {
	return &PackingEnqueuer{verification: v}
}

var _ feeddirectionapp.FeedPackingVerificationEnqueuer = (*PackingEnqueuer)(nil)

// EnqueueFeedPackingVerification maps the feeddirection request to a verification CreateItem. The single
// packing video travels on ONE item. CreateItem is idempotent on (tenant, idempotency_key), so a retry
// after a prior failure heals rather than duplicates.
func (e *PackingEnqueuer) EnqueueFeedPackingVerification(ctx context.Context, in feeddirectionapp.FeedPackingVerificationEnqueueRequest) error {
	// The subject names the SESSION and the PEN -- "Session 1 · Castro - 2" (maintainer decision
	// 2026-08-11, reverting the 2026-08-10 pen-only label). A pen produces two packing videos a day
	// and a verifier holding two cards for Castro - 2 must be able to tell which bag each one proves;
	// without the prefix the two items are indistinguishable in the queue.
	//
	// Degrades rather than composing a dangling separator: an unresolvable location leaves the bare
	// session, and a session-less request (which the write path now rejects) leaves the bare pen.
	loc := oploc.OperationalLocation{ShedName: in.ShedName, PartitionLabel: in.PartitionLabel}
	locDisplay := loc.Display()
	baseLabel := ""
	if in.SessionNo > 0 {
		baseLabel = fmt.Sprintf("Session %d", in.SessionNo)
	}
	var label *string
	switch {
	case baseLabel != "" && locDisplay != "":
		fullLabel := baseLabel + " · " + locDisplay
		label = &fullLabel
	case baseLabel != "":
		label = &baseLabel
	case locDisplay != "":
		label = &locDisplay
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: in.TenantID,
		Vertical: feeddirectiondomain.VerificationVerticalFeed,
		Module:   feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryPacking,
		// The verifier's subject for a feed-packing item is the SESSION and PEN being packed; surface
		// it so the detail view shows which bag's video is under review (shed/park/operator ride on
		// their own item fields). Backend owns this display string (dumb-renderer rule).
		SubjectLabel: label,
		Source: verificationdomain.SourceRef{
			Module:  feeddirectiondomain.VerificationModuleFeed,
			RefType: feeddirectiondomain.VerificationRefTypePacking,
			RefID:   in.CompletionID,
		},
		// BLIND PER-ITEM ENTRY (maintainer decision 2026-08-21, superseding the visible "Expected
		// ration" context row): the item carries the pen-session's feed item NAMES as one entry box
		// per item, and the verifier types the packed weight she can see for each before her
		// approve. The PLANNED quantities are deliberately absent from the item -- she must not be
		// able to copy them; the intended-vs-entered variance surfaces only on the leadership feed
		// analytics execution view. Empty when the frozen sheet was unreadable at submit, which the
		// verification service treats as a judge-the-video approve rather than stranding the item.
		MeasurementFields: packingMeasurementFields(in.MeasurementFields),
		// One media ref: the packing video.
		MediaRefs:  []string{in.PackingProofRef},
		OperatorID: ptrIfSet(in.OperatorID),
		ShedID:     ptrIfSet(in.ShedID),
		// The PEN as its own field, not only folded into the label. Packing composed the location
		// into SubjectLabel and left this column NULL, so anything filtering or grouping by pen --
		// as opposed to reading the display string -- missed every packing item.
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// packingMeasurementFields maps the producer's feed-item field list onto the verification item's
// generic per-item entry boxes, dropping any field without a key -- a keyless box could never be
// posted back. Labels are farm language composed by the producer (backend owns visible copy).
func packingMeasurementFields(fields []feeddirectionapp.PackingMeasurementField) []verificationdomain.MeasurementField {
	out := make([]verificationdomain.MeasurementField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Key) == "" {
			continue
		}
		out = append(out, verificationdomain.MeasurementField{Key: field.Key, Label: field.Label})
	}
	return out
}
