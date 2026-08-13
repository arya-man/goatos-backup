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
		// What the verifier is judging the video AGAINST: the frozen ration for THIS SESSION
		// ("Maize 12.5 kg · Soya 4 kg") and the head count it was computed from. One session's
		// figures, because one clip proves one bag -- handing her the day total would show twice what
		// the video should contain. Composed by the producer (dumb-renderer rule) and rendered
		// verbatim. Omitted when the sheet could not be read -- never a placeholder, which would read
		// as "no feed expected" rather than "not known".
		ContextRows: packingContextRows(in.RationSummary, in.HeadCountSummary),
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

// packingContextRows is the verifier's "what was expected" block for a packing proof.
//
// A row is emitted only when its value is known: a blank ration means the issued sheet could not be
// read, and rendering "Expected ration: —" would state that nothing was expected rather than that
// nothing is known. Labels are farm language, composed here because the backend owns visible copy.
func packingContextRows(rationSummary, headCountSummary string) []verificationdomain.ContextRow {
	rows := make([]verificationdomain.ContextRow, 0, 2)
	if strings.TrimSpace(rationSummary) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Expected ration", Value: rationSummary})
	}
	if strings.TrimSpace(headCountSummary) != "" {
		rows = append(rows, verificationdomain.ContextRow{Label: "Animals in this pen", Value: headCountSummary})
	}
	return rows
}
