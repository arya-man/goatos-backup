package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func strptr(s string) *string { return &s }

// A resolved cell (authored zero included) and a blocked cell, in one row, to prove the
// blocked-vs-zero distinction survives flatten -> reconstruct as two DISTINCT states.
func mixedRow() DirectionRow {
	return DirectionRow{
		ParkID: "park", ParkLabel: "CBE", ShedID: "shed-a", ShedLabel: "Shed A",
		ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
		SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: WorkflowNormal,
		SessionTotalKg: "1.000", OverduePending: false,
		Items: []ItemQuantity{
			// Authored zero: fully numeric, resolved.
			{FeedItem: "Milk", Status: QuantityResolved, QuantityKg: strptr("0.000"), GramsPerHead: strptr("0.000"), ShedFactor: strptr("1.0000")},
			// Resolved non-zero.
			{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000"), GramsPerHead: strptr("100.000"), ShedFactor: strptr("1.0000")},
			// Blocked: no number, a reason.
			{FeedItem: "Hay", Status: QuantityBlocked, BlockedReason: &BlockedReason{Code: BlockReasonNoRationRate, Detail: "no rate for Hay"}},
		},
	}
}

// The single most important property of storage: a blocked cell (nil quantity) and an authored zero
// ("0.000") are opposite states, and neither collapses into the other through a persist round trip.
func TestFlattenReconstructPreservesBlockedVsZero(t *testing.T) {
	t.Parallel()
	rows := []DirectionRow{mixedRow()}
	cells := FlattenRows(rows)

	// The blocked cell stores a NULL quantity and a reason; the zero stores 0.000 and no reason.
	var blocked, zero *StoredCell
	for i := range cells {
		switch cells[i].FeedItemLabel {
		case "Hay":
			blocked = &cells[i]
		case "Milk":
			zero = &cells[i]
		}
	}
	if blocked == nil || blocked.QuantityKg != nil {
		t.Fatalf("blocked cell must store a NULL quantity, got %+v", blocked)
	}
	if blocked.BlockedReasonCode == nil || *blocked.BlockedReasonCode != BlockReasonNoRationRate {
		t.Fatalf("blocked cell must carry a reason code, got %+v", blocked)
	}
	if zero == nil || zero.QuantityKg == nil || *zero.QuantityKg != "0.000" {
		t.Fatalf("authored zero must store 0.000, got %+v", zero)
	}
	if zero.BlockedReasonCode != nil {
		t.Fatalf("authored zero must NOT carry a blocked reason")
	}

	got := ReconstructRows(cells)
	if len(got) != 1 || len(got[0].Items) != 3 {
		t.Fatalf("reconstruct shape = %d rows, want 1 row of 3 items", len(got))
	}
	items := map[string]ItemQuantity{}
	for _, it := range got[0].Items {
		items[it.FeedItem] = it
	}
	if items["Hay"].Status != QuantityBlocked || items["Hay"].QuantityKg != nil {
		t.Errorf("Hay must reconstruct as blocked with nil quantity, got %+v", items["Hay"])
	}
	if items["Milk"].Status != QuantityResolved || items["Milk"].QuantityKg == nil || *items["Milk"].QuantityKg != "0.000" {
		t.Errorf("Milk must reconstruct as resolved 0.000, got %+v", items["Milk"])
	}
	if got[0].Blocked != true {
		t.Errorf("a row with a blocked cell must reconstruct with Blocked=true")
	}
}

// A multi-grain shed (two breeds sharing a ration group) must round-trip as two distinct rows in the
// same shed+session, which is why the natural key carries the grain discriminator.
func TestReconstructKeepsMultiGrainShedRowsDistinct(t *testing.T) {
	t.Parallel()
	rows := []DirectionRow{
		{ParkID: "p", ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000")}}},
		{ParkID: "p", ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Sojat", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "2.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("2.000")}}},
	}
	cells := FlattenRows(rows)
	// The two cells share (shed, session, feed_item) but differ by breed -- so their keys differ.
	if cells[0].Key() == cells[1].Key() {
		t.Fatalf("multi-grain cells collided on the natural key: %+v", cells[0].Key())
	}
	got := ReconstructRows(cells)
	if len(got) != 2 {
		t.Fatalf("multi-grain shed reconstructed %d rows, want 2", len(got))
	}
}

// An identical sheet fingerprints identically (so a re-issue is a no-op) and any change flips it.
func TestFingerprintRowsIsStableAndSensitive(t *testing.T) {
	t.Parallel()
	base := []DirectionRow{mixedRow()}
	if FingerprintRows(base) != FingerprintRows([]DirectionRow{mixedRow()}) {
		t.Fatal("identical sheets must fingerprint identically")
	}
	changed := []DirectionRow{mixedRow()}
	changed[0].Items[1].QuantityKg = strptr("1.500")
	if FingerprintRows(base) == FingerprintRows(changed) {
		t.Fatal("a changed quantity must change the fingerprint")
	}
	// A blocked-vs-zero flip must move the fingerprint too.
	flip := []DirectionRow{mixedRow()}
	flip[0].Items[0].Status = QuantityBlocked
	flip[0].Items[0].QuantityKg = nil
	flip[0].Items[0].BlockedReason = &BlockedReason{Code: BlockReasonNoRationRate}
	if FingerprintRows(base) == FingerprintRows(flip) {
		t.Fatal("a resolved->blocked flip must change the fingerprint")
	}
}

// An amendment must report exactly the changed and removed cells, and the affected sheds.
func TestDiffCellsReportsChangedRemovedAndAffectedSheds(t *testing.T) {
	t.Parallel()
	stored := FlattenRows([]DirectionRow{
		{ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.000")}}},
		{ShedID: "shed-b", ShedTag: "Non-Pregnant", Breed: "Sirohi", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "2.000",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("2.000")}}},
	})
	// shed-a's quantity changed; shed-b removed entirely.
	generated := FlattenRows([]DirectionRow{
		{ShedID: "shed-a", ShedTag: "Non-Pregnant", Breed: "Beetal", SessionNo: 1, Workflow: WorkflowNormal, SessionTotalKg: "1.500",
			Items: []ItemQuantity{{FeedItem: "Concentrate", Status: QuantityResolved, QuantityKg: strptr("1.500")}}},
	})

	diff := DiffCells(stored, generated)
	if !diff.HasChanges() {
		t.Fatal("expected changes")
	}
	if len(diff.Changed) != 1 || diff.Changed[0].ShedID != "shed-a" {
		t.Fatalf("changed = %+v, want only shed-a", diff.Changed)
	}
	if len(diff.RemovedKeys) != 1 || diff.RemovedKeys[0].ShedID != "shed-b" {
		t.Fatalf("removed = %+v, want only shed-b", diff.RemovedKeys)
	}
	if len(diff.AffectedShedIDs) != 2 {
		t.Fatalf("affected sheds = %v, want both", diff.AffectedShedIDs)
	}

	// An identical recompute is a no-op diff.
	if DiffCells(stored, stored).HasChanges() {
		t.Fatal("an identical recompute must diff to no changes")
	}
}

// The issue instant is the direction_time on the day BEFORE the feed day, in Asia/Kolkata.
func TestExpectedIssueInstantIsDMinusOneAtDirectionTime(t *testing.T) {
	t.Parallel()
	clock := WorkflowClock{Workflow: WorkflowNormal, DirectionTime: "07:00:00", CorrectionTime: "14:00:00"}
	got, err := clock.ExpectedIssueInstant("2026-07-30")
	if err != nil {
		t.Fatalf("ExpectedIssueInstant: %v", err)
	}
	want := time.Date(2026, 7, 29, 7, 0, 0, 0, biztime.DefaultLocation())
	if !got.Equal(want) {
		t.Fatalf("issue instant = %s, want %s (D-1 at 07:00 IST)", got, want)
	}
}
