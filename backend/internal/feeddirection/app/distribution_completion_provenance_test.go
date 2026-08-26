package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Who uploaded each of a pen-session's three proofs, and when — the detail behind the leadership
// execution table (maintainer decision 2026-08-26).
//
// The thing under test is the SHAPE of the lookup as much as its result: one batched call for the
// whole day, never one per row. A per-row call passes every assertion about the rendered values and
// still turns a 208-pen park-day into 208 round trips one adapter layer down, which is exactly the
// n-plus-one-fanout the raw-driver guard cannot see.

// describerProofValidator implements BOTH seams, the way the real postgres-backed validator does,
// so this also proves WithProofValidator picks the describer up. That wiring is a type assertion in
// one setter; if someone replaces it with a second explicit setter and forgets a composition root,
// every proof renders with a blank uploader and no time — indistinguishable from "nobody filmed it".
type describerProofValidator struct {
	fakeProofValidator
	calls   int
	lastIDs []string
	uploads map[string]ports.ProofUpload
	err     error
}

func (d *describerProofValidator) DescribeProofUploads(
	_ context.Context, _ string, proofIDs []string,
) (map[string]ports.ProofUpload, error) {
	d.calls++
	d.lastIDs = append([]string(nil), proofIDs...)
	if d.err != nil {
		return nil, d.err
	}
	return d.uploads, nil
}

func completionRow(pen string, refs ...string) domain.DistributionCompletionRow {
	row := domain.DistributionCompletionRow{
		OperationalLocationDisplay: pen,
		Status:                     domain.DistributionCompletionCompleted,
	}
	for i, slot := range domain.DistributionSlotOrder {
		ref := ""
		if i < len(refs) {
			ref = refs[i]
		}
		row.Proofs = append(row.Proofs, domain.DistributionProofSlot{FieldKey: slot, ProofRef: ref})
	}
	return row
}

func TestDistributionProofProvenanceIsResolvedInOneBatchedCall(t *testing.T) {
	at := time.Date(2026, 8, 24, 15, 45, 0, 0, time.UTC)
	describer := &describerProofValidator{uploads: map[string]ports.ProofUpload{
		"weight-a": {ProofID: "weight-a", UploadedAt: at, UploadedByName: "Santosh", MimeType: "image/jpeg"},
		"feed-a":   {ProofID: "feed-a", UploadedAt: at.Add(17 * time.Minute), UploadedByName: "Rajniti", MimeType: "video/mp4"},
		"water-b":  {ProofID: "water-b", UploadedAt: at.Add(40 * time.Minute), UploadedByName: "", MimeType: "video/mp4"},
	}}
	svc := NewService(nil, nil).WithProofValidator(describer)

	rows := []domain.DistributionCompletionRow{
		// Two pens' worth of references, and one reference ("ghost") that resolves to nothing.
		completionRow("Castro 1", "weight-a", "feed-a", "ghost"),
		completionRow("Godel 2 - Part 1", "", "", "water-b"),
	}
	svc.describeDistributionProofs(context.Background(), "tenant-1", rows)

	// ONE call for both rows — the batching contract.
	if describer.calls != 1 {
		t.Fatalf("want exactly 1 batched DescribeProofUploads call for the whole table, got %d", describer.calls)
	}
	// Only the references that exist are asked for; an empty slot is not a proof id.
	want := []string{"weight-a", "feed-a", "ghost", "water-b"}
	if len(describer.lastIDs) != len(want) {
		t.Fatalf("asked for %v, want %v", describer.lastIDs, want)
	}
	for i, id := range want {
		if describer.lastIDs[i] != id {
			t.Fatalf("asked for %v, want %v", describer.lastIDs, want)
		}
	}

	// Provenance lands on the RIGHT slot of the RIGHT row.
	weight := rows[0].Proofs[0]
	if weight.UploadedByName != "Santosh" || weight.UploadedAt == nil || !weight.UploadedAt.Equal(at) {
		t.Errorf("weight photo provenance: %+v", weight)
	}
	if rows[0].Proofs[1].UploadedByName != "Rajniti" {
		t.Errorf("feed video provenance: %+v", rows[0].Proofs[1])
	}

	// A reference that resolves to NOTHING keeps its ref and gains no fabricated provenance —
	// the row must not read as a proof uploaded by nobody at the zero instant.
	ghost := rows[0].Proofs[2]
	if ghost.ProofRef != "ghost" || ghost.UploadedAt != nil || ghost.UploadedByName != "" {
		t.Errorf("unresolved reference must gain nothing, got %+v", ghost)
	}

	// An EMPTY slot stays empty and stays PRESENT: the missing video is the point of the screen.
	empty := rows[1].Proofs[0]
	if empty.ProofRef != "" || empty.UploadedAt != nil {
		t.Errorf("empty slot must stay empty, got %+v", empty)
	}
	if len(rows[1].Proofs) != 3 {
		t.Errorf("every row carries all three slots, got %d", len(rows[1].Proofs))
	}
	// A resolved proof whose uploader cannot be named keeps the time and leaves the name blank —
	// an id is never rendered in a person's place.
	if rows[1].Proofs[2].UploadedAt == nil || rows[1].Proofs[2].UploadedByName != "" {
		t.Errorf("unnameable uploader: %+v", rows[1].Proofs[2])
	}
}

func TestDistributionProofProvenanceFailureLeavesTheTableStanding(t *testing.T) {
	// The table's own answer — which pen was fed, which was not — comes from the completion rows.
	// A proof-module failure must cost the provenance detail, never the screen.
	describer := &describerProofValidator{err: errors.New("proof module down")}
	svc := NewService(nil, nil).WithProofValidator(describer)
	rows := []domain.DistributionCompletionRow{completionRow("Castro 1", "weight-a", "feed-a", "water-a")}

	svc.describeDistributionProofs(context.Background(), "tenant-1", rows)

	if len(rows[0].Proofs) != 3 || rows[0].Proofs[0].ProofRef != "weight-a" {
		t.Fatalf("rows must survive a describer failure: %+v", rows[0].Proofs)
	}
	if rows[0].Proofs[0].UploadedAt != nil {
		t.Error("a failed lookup must not invent provenance")
	}

	// No describer wired at all (a pure-generation build) behaves the same way.
	NewService(nil, nil).describeDistributionProofs(context.Background(), "tenant-1", rows)
	if len(rows[0].Proofs) != 3 {
		t.Fatal("an unwired describer must leave the rows untouched")
	}
}
