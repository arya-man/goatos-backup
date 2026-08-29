package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// ---------------------------------------------------------------------------
// The packed-against snapshot + the session-specific reopen reason (maintainer decision 2026-08-29,
// closing the STG 2026-08-28 confusion: a reopened card silently showed the corrected quantity with
// a generic sentence, and nothing anywhere kept what the operator had actually packed against).
// ---------------------------------------------------------------------------

type nullPackingEnqueuer struct{ calls int }

func (e *nullPackingEnqueuer) EnqueueFeedPackingVerification(_ context.Context, _ FeedPackingVerificationEnqueueRequest) error {
	e.calls++
	return nil
}

// The submit reads the frozen sheet ONCE and snapshots exactly what the card showed -- head count,
// session total, and the per-item directed quantities -- onto the completion write. The assertion
// compares against the SERVED worklist row, so the snapshot and the card cannot disagree.
func TestCompletePackingSnapshotsThePackedAgainstSheet(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, _, packing := newPackingLifecycleService(asOf)
	svc = svc.WithPackingVerificationEnqueuer(&nullPackingEnqueuer{})
	ctx := context.Background()
	if _, err := svc.IssueDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}

	page, err := svc.PackingWorklist(ctx, domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	var card *domain.PackingRow
	for i := range page.Items {
		if page.Items[i].ShedID == shedB {
			card = &page.Items[i]
			break
		}
	}
	if card == nil {
		t.Fatal("no shed B packing line on the issued sheet")
	}

	if _, err := svc.CompletePacking(ctx, CompletePackingInput{
		TenantID: testTenant, ParkID: testPark, ShedID: card.ShedID, PartitionLabel: card.PartitionLabel,
		SessionNo: card.SessionNo, TargetDate: feedDayTarget(), Workflow: domain.WorkflowNormal,
		PackingProofRef: "proof-1", IdempotencyKey: "k1",
	}); err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}
	if len(packing.completeCalls) != 1 {
		t.Fatalf("CompletePacking store writes = %d, want 1", len(packing.completeCalls))
	}
	snap := packing.completeCalls[0].PackedAgainst
	if snap == nil {
		t.Fatal("the submit must snapshot the frozen sheet it was packed against")
	}
	if snap.HeadCount != card.HeadCount {
		t.Fatalf("snapshot head count = %d, want the card's %d", snap.HeadCount, card.HeadCount)
	}
	if snap.TotalKg != card.TotalKg {
		t.Fatalf("snapshot total = %q, want the card's %q", snap.TotalKg, card.TotalKg)
	}
	if len(snap.Items) == 0 {
		t.Fatal("snapshot carries no items for a card that directs feed")
	}
	for _, item := range snap.Items {
		if item.Key == "" || item.Label == "" || item.QuantityKg == "" {
			t.Fatalf("snapshot item incompletely populated: %+v", item)
		}
	}
}

// THE STG 2026-08-28 CASE. A bag was packed and filmed against the morning sheet; animals shifted
// in before the correction. The reopened row must carry a sentence naming BOTH numbers -- what the
// bag was packed for and what it must be now -- not the generic fallback that left the packer
// staring at a silently rewritten card.
func TestAfternoonCorrectionReopenReasonNamesOldAndNewQuantities(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, counts, packing := newPackingLifecycleService(asOf)
	ctx := context.Background()
	if _, err := svc.IssueDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}
	page, err := svc.PackingWorklist(ctx, domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	var card *domain.PackingRow
	for i := range page.Items {
		if page.Items[i].ShedID == shedB {
			card = &page.Items[i]
			break
		}
	}
	if card == nil {
		t.Fatal("no shed B packing line on the issued sheet")
	}

	// The packer already submitted this bag: the stored row carries the packed-against snapshot the
	// submit wrote (asserted separately above).
	oldHeads := card.HeadCount
	packing.statuses = []ports.PackingCompletionStatus{{
		ShedID: card.ShedID, PartitionLabel: card.PartitionLabel, SessionNo: card.SessionNo,
		Workflow: domain.WorkflowNormal, Status: domain.PackingStatusPendingVerification,
		PackedHeadCount: &oldHeads, PackedTotalKg: card.TotalKg,
	}}

	// Animals shift into shed B before the 14:00 correction.
	counts.grains[shedB] = []domain.ShedGrain{{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 40}}
	if _, err := svc.AmendDirection(ctx, IssueRequest{
		TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal,
		AsOf: istInstant(2026, 7, 29, 14),
	}); err != nil {
		t.Fatalf("AmendDirection: %v", err)
	}
	if len(packing.reopenCalls) != 1 {
		t.Fatalf("reopen calls = %d, want 1", len(packing.reopenCalls))
	}
	call := packing.reopenCalls[0]
	// The generic fallback survives untouched for rows no context matches.
	if call.Reason != packingReopenedReason {
		t.Fatalf("fallback reason = %q, want the generic sentence", call.Reason)
	}
	var reopenCtx *ports.ReopenSessionContext
	for i := range call.SessionContexts {
		sc := &call.SessionContexts[i]
		if sc.ShedID == card.ShedID && sc.SessionNo == card.SessionNo {
			reopenCtx = sc
			break
		}
	}
	if reopenCtx == nil {
		t.Fatalf("no session context for the reopened pen-session; contexts = %+v", call.SessionContexts)
	}
	oldPhrase := packedBagPhrase(card.TotalKg, oldHeads)
	if oldPhrase == "" || !strings.Contains(reopenCtx.Reason, "This bag was "+oldPhrase) {
		t.Fatalf("reason %q does not name the packed-against quantities (%q)", reopenCtx.Reason, oldPhrase)
	}
	if !strings.Contains(reopenCtx.Reason, "; it is now ") || !strings.Contains(reopenCtx.Reason, "for 40 animals") {
		t.Fatalf("reason %q does not name the corrected quantities", reopenCtx.Reason)
	}
	if !strings.Contains(reopenCtx.Reason, "record a new video") {
		t.Fatalf("reason %q drops the re-shoot instruction", reopenCtx.Reason)
	}
	if reopenCtx.NewHeadCount != 40 {
		t.Fatalf("context new head count = %d, want 40", reopenCtx.NewHeadCount)
	}
	if reopenCtx.OperationalLocationDisplay == "" || reopenCtx.NewTotalKg == "" {
		t.Fatalf("context display facts incompletely populated: %+v", reopenCtx)
	}
	// Copy firewall: operator-facing text must not leak implementation vocabulary.
	for _, banned := range []string{"amend", "correction", "shifting", "projection", "workflow", "row_version", "snapshot"} {
		if strings.Contains(strings.ToLower(reopenCtx.Reason), banned) {
			t.Fatalf("reason %q leaks the internal word %q", reopenCtx.Reason, banned)
		}
	}
}

// A row submitted before the snapshot existed (or whose sheet was unreadable at submit) still gets
// a SPECIFIC sentence -- the corrected numbers -- rather than a fabricated old value or the bare
// generic fallback.
func TestAfternoonCorrectionReopenReasonDegradesWithoutASnapshot(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, counts, packing := newPackingLifecycleService(asOf)
	ctx := context.Background()
	if _, err := svc.IssueDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}
	// A legacy submitted row: no packed-against snapshot at all.
	packing.statuses = []ports.PackingCompletionStatus{{
		ShedID: shedB, PartitionLabel: "", SessionNo: 1,
		Workflow: domain.WorkflowNormal, Status: domain.PackingStatusPendingVerification,
	}}
	counts.grains[shedB] = []domain.ShedGrain{{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 40}}
	if _, err := svc.AmendDirection(ctx, IssueRequest{
		TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal,
		AsOf: istInstant(2026, 7, 29, 14),
	}); err != nil {
		t.Fatalf("AmendDirection: %v", err)
	}
	if len(packing.reopenCalls) != 1 {
		t.Fatalf("reopen calls = %d, want 1", len(packing.reopenCalls))
	}
	var reopenCtx *ports.ReopenSessionContext
	for i := range packing.reopenCalls[0].SessionContexts {
		sc := &packing.reopenCalls[0].SessionContexts[i]
		if sc.ShedID == shedB && sc.SessionNo == 1 {
			reopenCtx = sc
			break
		}
	}
	if reopenCtx == nil {
		t.Fatal("no session context for the reopened pen-session")
	}
	if !strings.Contains(reopenCtx.Reason, "This bag is now ") || !strings.Contains(reopenCtx.Reason, "for 40 animals") {
		t.Fatalf("reason %q must carry the corrected quantities", reopenCtx.Reason)
	}
	if strings.Contains(reopenCtx.Reason, "This bag was ") {
		t.Fatalf("reason %q invents an old value the row never recorded", reopenCtx.Reason)
	}
}

// displayKg / packedBagPhrase are the number-rendering halves of the reopen sentence; a wrong trim
// here would tell a packer "40 kg" for a 400 kg bag.
func TestReopenCopyNumberRendering(t *testing.T) {
	t.Parallel()
	kgCases := map[string]string{
		"24.000": "24", "4.500": "4.5", "400": "400", "400.000": "400",
		"0.000": "", "": "", "junk": "", "-3.000": "",
	}
	for in, want := range kgCases {
		if got := displayKg(in); got != want {
			t.Fatalf("displayKg(%q) = %q, want %q", in, got, want)
		}
	}
	phraseCases := []struct {
		kg    string
		heads int64
		want  string
	}{
		{"24.000", 12, "24 kg for 12 animals"},
		{"4.500", 1, "4.5 kg for 1 animal"},
		{"", 12, "for 12 animals"},
		{"24.000", 0, "24 kg"},
		{"", 0, ""},
	}
	for _, c := range phraseCases {
		if got := packedBagPhrase(c.kg, c.heads); got != c.want {
			t.Fatalf("packedBagPhrase(%q, %d) = %q, want %q", c.kg, c.heads, got, c.want)
		}
	}
}
