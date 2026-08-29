package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Feed PACKING verification gate -- the app half (maintainer decision, 2026-07-26, SUPERSEDING the
// "packing stays instant, no verifier" rule). A feed PACKING shed-session now passes through the
// generic Verification module before it is completed: the operator submits ONE mandatory packing video,
// which writes a 'pending_verification' feed_packing_completions row and enqueues ONE verification item;
// the session is 'completed' only when a verifier approves. This is a SEPARATE record from the old
// instant packing path (CompleteSession / feed_direction_session_completions), which is left inert, and
// from the distribution gate. See docs/decisions/feed-distribution-verification.md.

// ErrPackingEnqueuerNotWired is returned when a packing completion cannot enqueue its verification item
// because the enqueue seam was never wired -- a composition bug, surfaced loudly rather than silently
// stranding a pending_verification session.
var ErrPackingEnqueuerNotWired = errors.New("feeddirection: packing verification enqueuer is not wired")

// FeedPackingVerificationEnqueuer enqueues the mandatory-proof verification item for a submitted feed
// packing session (mirrors FeedDistributionVerificationEnqueuer). The composition layer adapts the
// verification module's CreateItem to this narrow port so feeddirection never touches verification's
// tables directly.
type FeedPackingVerificationEnqueuer interface {
	EnqueueFeedPackingVerification(ctx context.Context, in FeedPackingVerificationEnqueueRequest) error
}

// FeedPackingVerificationEnqueueRequest is one packing completion (its video) handed to the verifier
// queue.
type FeedPackingVerificationEnqueueRequest struct {
	TenantID        string
	CompletionID    string
	ParkID          string
	ShedID          string
	ShedName        string
	PartitionLabel  string
	SessionNo       int32
	Workflow        string
	TargetDate      time.Time
	PackingProofRef string
	OperatorID      string
	CapturedAt      time.Time
	IdempotencyKey  string
	// MeasurementFields is the pen-session's feed item list -- KEY (normalized config key) plus
	// display LABEL, NAMES ONLY, in the frozen sheet's order -- carried onto the verifier's item as
	// one entry box per feed item (maintainer decision 2026-08-21, superseding the visible
	// "Expected ration" context row). The verifier types the packed weight she can see for each
	// item and the approve carries the numbers; the PLANNED quantities are deliberately absent so
	// she cannot copy them -- the intended-vs-entered variance belongs to the leadership feed
	// analytics execution view, never to her screen.
	//
	// Read from the ISSUED sheet at submit time and stored on the item, so re-authoring the feed
	// config afterwards cannot change which boxes she is asked to fill. Empty when the sheet
	// cannot be read: a completion must never fail because its decoration could not be composed,
	// and a fields-less item degrades to a judge-the-video approve.
	MeasurementFields []PackingMeasurementField
}

// PackingMeasurementField is one entry box on the verifier's packing item: the normalized feed
// item key (NormalizeConfigKey output, the same key the frozen sheet rows carry) and the display
// label the box is captioned with.
type PackingMeasurementField struct {
	Key   string
	Label string
}

// CompletePackingInput is the app-level packing completion request the HTTP handler builds from the
// body plus the authenticated actor context.
type CompletePackingInput struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen the operator actually worked ("2", "Part 3"); empty for an
	// undivided shed. Carried end-to-end so ONE pen's proof closes ONE pen -- see migration 000137.
	PartitionLabel string
	// SessionNo is the feeding session the operator packed and filmed. Carried end-to-end so ONE
	// session's proof closes ONE session and the sibling bag is still owed.
	SessionNo       int32
	TargetDate      time.Time
	Workflow        string
	PackingProofRef string
	CompletedBy     string
	IdempotencyKey  string
	ActorID         string
	ActorType       string
	TraceID         string
}

// CompletePacking records a packing shed-session's ONE mandatory video at 'pending_verification' and
// enqueues one verifier-queue item. It validates the request on the same terms as CompleteDistribution,
// then requires the video, validates it through the proof validator when wired, fails closed when the
// store or enqueue seam is missing, and enqueues only when the row actually enters pending_verification
// on this call. It completes NOTHING -- the session is completed only when a verifier approves (the
// consumer's ApplyVerifiedPacking).
func (s *Service) CompletePacking(ctx context.Context, in CompletePackingInput) (ports.CompletePackingResult, error) {
	if s.packing == nil {
		return ports.CompletePackingResult{}, ports.ErrPackingStoreUnavailable
	}
	if s.packingEnqueuer == nil {
		// Fail closed: without the verifier-queue seam a completion would flip a session to
		// pending_verification with nothing for a verifier to act on.
		return ports.CompletePackingResult{}, ErrPackingEnqueuerNotWired
	}

	// Write path: the route already clamped the park to the caller's grant. See CompleteDistribution.
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return ports.CompletePackingResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.Workflow = strings.TrimSpace(in.Workflow)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.PackingProofRef = strings.TrimSpace(in.PackingProofRef)

	if in.TenantID == "" || in.ParkID == "" {
		return ports.CompletePackingResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return ports.CompletePackingResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return ports.CompletePackingResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	// A packing completion names a REAL session. Session 0 is not "the whole day" -- that reading was
	// the 2026-08-10 pen-day grain and it is reverted; accepting 0 now would write a row no worklist
	// line matches, so the operator's bag would still show as owed.
	if in.SessionNo < 1 {
		return ports.CompletePackingResult{}, ports.ErrInvalidSession
	}
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return ports.CompletePackingResult{}, ports.ErrWorkflowRequired
	default:
		return ports.CompletePackingResult{}, ports.ErrInvalidWorkflow
	}
	if in.IdempotencyKey == "" {
		return ports.CompletePackingResult{}, ports.ErrIdempotencyRequired
	}

	// The packing VIDEO is mandatory -- reject before any state changes (there is nothing for a verifier
	// to approve without it).
	if in.PackingProofRef == "" {
		return ports.CompletePackingResult{}, ports.ErrPackingProofRequired
	}

	// When a validator is wired, the proof id must resolve to a real, completed, tenant-owned upload
	// before the completion is written.
	if s.proofs != nil {
		if err := s.proofs.ValidateFeedProofs(ctx, in.TenantID, []string{in.PackingProofRef}); err != nil {
			return ports.CompletePackingResult{}, err
		}
	}

	// ONE read of the frozen sheet serves both decorations: the verifier's measurement fields and
	// the packed-against snapshot stored on the row (migration 000222) -- what the operator's card
	// directed at the moment the bag was filled, kept so the afternoon correction can say "you
	// packed 4 kg for 2 animals; this bag is now 24 kg for 12" instead of silently rewriting the
	// card. Both are fail-open: an unreadable sheet yields a nil row, a nil snapshot, and no fields.
	sheetRow, sheetRowFound := s.packingSheetRow(ctx, in)
	var packedAgainst *ports.PackedAgainstSnapshot
	if sheetRowFound {
		packedAgainst = packedAgainstFromRow(sheetRow)
	}

	result, err := s.packing.CompletePacking(ctx, ports.CompletePackingParams{
		TenantID:        in.TenantID,
		ParkID:          in.ParkID,
		ShedID:          in.ShedID,
		PartitionLabel:  in.PartitionLabel,
		SessionNo:       in.SessionNo,
		TargetDate:      in.TargetDate,
		Workflow:        in.Workflow,
		PackingProofRef: in.PackingProofRef,
		CompletedBy:     strings.TrimSpace(in.CompletedBy),
		PackedAgainst:   packedAgainst,
		IdempotencyKey:  in.IdempotencyKey,
		ActorID:         in.ActorID,
		ActorType:       in.ActorType,
		TraceID:         in.TraceID,
	})
	if err != nil {
		return ports.CompletePackingResult{}, err
	}

	// Enqueue the verifier item ONLY on a fresh pending transition (a new submit or a rework re-submit).
	// An idempotent replay or an already-pending/already-completed no-op enqueues nothing. The enqueue is
	// idempotent on (completion_id + row_version), so a retry after a prior enqueue failure heals rather
	// than duplicates: the completion is not "done" for the operator until the item is queued.
	if result.NewlyPending {
		var fields []PackingMeasurementField
		if sheetRowFound {
			fields = packingEntryFields(sheetRow.Items)
		}
		if enqErr := s.packingEnqueuer.EnqueueFeedPackingVerification(ctx, FeedPackingVerificationEnqueueRequest{
			TenantID:          in.TenantID,
			CompletionID:      result.CompletionID,
			ParkID:            in.ParkID,
			ShedID:            in.ShedID,
			ShedName:          result.ShedName,
			PartitionLabel:    result.PartitionLabel,
			SessionNo:         in.SessionNo,
			Workflow:          in.Workflow,
			TargetDate:        in.TargetDate,
			PackingProofRef:   in.PackingProofRef,
			OperatorID:        strings.TrimSpace(in.CompletedBy),
			MeasurementFields: fields,
			CapturedAt:        s.now().UTC(),
			// Keyed to the completion + its row_version so a rework re-submit (row_version bumped) enqueues a
			// fresh item while a retry of the same submit collapses onto one queue item.
			IdempotencyKey: "feed-packing-verification:" + result.CompletionID + ":" + strconv.Itoa(int(result.RowVersion)),
		}); enqErr != nil {
			return ports.CompletePackingResult{}, enqErr
		}
	}
	return result, nil
}

// packingSheetRow reads the FROZEN issued sheet and returns this PEN-SESSION's packing row -- the
// single sheet read behind both submit-time decorations: the verifier's measurement fields
// (packingEntryFields; NAMES ONLY per the 2026-08-21 blind-entry decision -- the verifier enters
// what she can see packed, and the intended-vs-entered variance surfaces only on the leadership
// feed analytics execution view) and the packed-against snapshot (packedAgainstFromRow).
//
// ONE SESSION'S row, not the day's -- one clip proves one bag.
//
// FAIL-OPEN, deliberately. A completion is the operator's work reaching the server; it must never
// fail because a decoration could not be composed. An unreadable or never-issued sheet, or a
// pen-session the sheet does not list, returns (zero, false): the completion still lands with no
// snapshot and the verifier item degrades to a judge-the-video approve (the verification service
// exempts a fields-less per-item item from the entries requirement for exactly this case).
//
// It reads the same frozen rows the packing worklist serves, ONCE per completion (a submit, not a
// list), bounded by the park's pens x sessions x items -- never by herd size.
func (s *Service) packingSheetRow(ctx context.Context, in CompletePackingInput) (domain.PackingRow, bool) {
	if s.issues == nil {
		return domain.PackingRow{}, false
	}
	feedDay := biztime.BusinessDate(in.TargetDate)
	scopeRows, _, served, err := s.loadServedRows(ctx, in.TenantID, in.ParkID, feedDay, in.Workflow)
	if err != nil || !served {
		return domain.PackingRow{}, false
	}
	// Match on the SAME identity the completion is keyed by -- shed + pen + session -- with the pen
	// normalized the way the natural key normalizes it, so "Part 3" and "part 3" are one pen.
	wantPartition := domain.PartitionMatchKey(in.PartitionLabel)
	for _, row := range domain.BuildPackingRows(scopeRows, domain.DistinctFeedItems(scopeRows)) {
		if row.ShedID != in.ShedID || row.SessionNo != in.SessionNo {
			continue
		}
		if domain.PartitionMatchKey(row.PartitionLabel) != wantPartition {
			continue
		}
		return row, true
	}
	return domain.PackingRow{}, false
}

// packedAgainstFromRow projects one frozen packing row into the snapshot stored on the completion:
// the head count and directed quantities the operator's card showed when the bag was filled. Items
// keep the same resolved-positive filter as the verifier's entry boxes (packingEntryFields) -- the
// bag's actual directed contents, not the grid's zero-quantity shape cells.
func packedAgainstFromRow(row domain.PackingRow) *ports.PackedAgainstSnapshot {
	snapshot := &ports.PackedAgainstSnapshot{HeadCount: row.HeadCount, TotalKg: strings.TrimSpace(row.TotalKg)}
	for _, item := range row.Items {
		if item.Status != domain.QuantityResolved || item.QuantityKg == nil {
			continue
		}
		kg, err := strconv.ParseFloat(strings.TrimSpace(*item.QuantityKg), 64)
		if err != nil || kg <= 0 {
			continue
		}
		snapshot.Items = append(snapshot.Items, ports.PackedItemSnapshot{
			Key:        domain.NormalizeConfigKey(item.FeedItem),
			Label:      item.FeedItem,
			QuantityKg: strings.TrimSpace(*item.QuantityKg),
		})
	}
	return snapshot
}

// packingEntryFields turns ONE frozen packing row's items into the verifier's entry boxes:
// only items the sheet actually directs this bag to contain (resolved, positive quantity).
//
// The frozen sheet carries every feed item the pen's ration rows mention, INCLUDING zero-quantity
// cells -- the grid shape, not the bag's contents. Shipping those as boxes made the verifier type
// 0 for every item the shed was never fed (maintainer report 2026-08-22), which is busywork that
// also buries the readings that matter. A blocked item is likewise omitted: it carries no directed
// quantity, and a bag whose line is blocked is not packable in the first place.
func packingEntryFields(items []domain.ItemQuantity) []PackingMeasurementField {
	fields := make([]PackingMeasurementField, 0, len(items))
	for _, item := range items {
		if item.Status != domain.QuantityResolved || item.QuantityKg == nil {
			continue
		}
		kg, err := strconv.ParseFloat(strings.TrimSpace(*item.QuantityKg), 64)
		if err != nil || kg <= 0 {
			continue
		}
		fields = append(fields, PackingMeasurementField{
			// The SAME normalization the worklist and the frozen sheet use, so the verifier's
			// reading lands on the key the variance read joins by.
			Key:   domain.NormalizeConfigKey(item.FeedItem),
			Label: item.FeedItem,
		})
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
