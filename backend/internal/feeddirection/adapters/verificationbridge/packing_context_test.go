package verificationbridge

import (
	"context"
	"testing"
	"time"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// A feed packing item carries the pen-session's feed items as BLIND ENTRY BOXES (maintainer
// decision 2026-08-21, superseding the visible "Expected ration" context row): names only, one box
// per item, and NEVER the planned quantities -- the verifier must not be able to copy them, and the
// intended-vs-entered variance belongs to the leadership execution view alone. These assertions are
// on the values reaching CreateItem because a "does the field exist" check passed throughout the
// STG 2026-08-09 outage this file was born from.

type capturingCreator struct{ last verificationdomain.CreateItem }

func (c *capturingCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	c.last = in
	return verificationdomain.CreateItemResult{}, nil
}

func enqueuePacking(t *testing.T, in feeddirectionapp.FeedPackingVerificationEnqueueRequest) verificationdomain.CreateItem {
	t.Helper()
	creator := &capturingCreator{}
	if err := NewPacking(creator).EnqueueFeedPackingVerification(context.Background(), in); err != nil {
		t.Fatalf("EnqueueFeedPackingVerification() error = %v", err)
	}
	return creator.last
}

func basePackingRequest() feeddirectionapp.FeedPackingVerificationEnqueueRequest {
	return feeddirectionapp.FeedPackingVerificationEnqueueRequest{
		TenantID:       "11111111-1111-4111-8111-111111111111",
		CompletionID:   "22222222-2222-4222-8222-222222222222",
		ParkID:         "33333333-3333-4333-8333-333333333333",
		ShedID:         "44444444-4444-4444-8444-444444444444",
		ShedName:       "Mandela 1",
		PartitionLabel: "Part 2",
		// A real request ALWAYS carries a session: the write path rejects a missing or zero value with
		// ErrInvalidSession and the table's CHECK refuses it (maintainer decision 2026-08-11). Omitting
		// it here is what let this file keep asserting the reverted pen-day label while passing --
		// every case below was exercising a request production cannot produce.
		SessionNo:       1,
		Workflow:        "normal",
		TargetDate:      time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		PackingProofRef: "55555555-5555-4555-8555-555555555555",
		IdempotencyKey:  "feed-packing-verification:22222222-2222-4222-8222-222222222222:1",
	}
}

func TestPackingItemCarriesEntryBoxesAndNeverThePlannedQuantities(t *testing.T) {
	req := basePackingRequest()
	// THIS SESSION's items, in the frozen sheet's order (one clip proves one bag). Names only.
	req.MeasurementFields = []feeddirectionapp.PackingMeasurementField{
		{Key: "maize", Label: "Maize"},
		{Key: "soya", Label: "Soya"},
	}

	got := enqueuePacking(t, req)

	if len(got.MeasurementFields) != 2 {
		t.Fatalf("measurement fields = %+v, want one entry box per feed item", got.MeasurementFields)
	}
	if got.MeasurementFields[0] != (verificationdomain.MeasurementField{Key: "maize", Label: "Maize"}) ||
		got.MeasurementFields[1] != (verificationdomain.MeasurementField{Key: "soya", Label: "Soya"}) {
		t.Errorf("measurement fields = %+v, want the sheet's items in order, names only", got.MeasurementFields)
	}
	// BLIND ENTRY: no planned quantity may reach the verifier's item through any side door. The
	// context rows used to carry "Expected ration: Maize 12.5 kg · Soya 4 kg"; they must stay empty.
	if len(got.ContextRows) != 0 {
		t.Errorf("context rows = %+v, want none -- the planned ration is leadership-only now", got.ContextRows)
	}
}

// An unreadable sheet yields NO boxes rather than invented ones; the verification service treats a
// fields-less packing item as a judge-the-video approve, so the item is not stranded.
func TestPackingItemOmitsBoxesWhenTheSheetCouldNotBeRead(t *testing.T) {
	got := enqueuePacking(t, basePackingRequest())

	if len(got.MeasurementFields) != 0 {
		t.Errorf("measurement fields = %+v, want none when the issued sheet could not be read", got.MeasurementFields)
	}
	if len(got.ContextRows) != 0 {
		t.Errorf("context rows = %+v, want none", got.ContextRows)
	}
}

// A field without a key is dropped rather than shipped: a keyless box could never be posted back
// on the verdict, so it would render as an unfillable requirement.
func TestPackingItemDropsKeylessFields(t *testing.T) {
	req := basePackingRequest()
	req.MeasurementFields = []feeddirectionapp.PackingMeasurementField{
		{Key: "  ", Label: "Ghost"},
		{Key: "maize", Label: "Maize"},
	}

	got := enqueuePacking(t, req)

	if len(got.MeasurementFields) != 1 || got.MeasurementFields[0].Key != "maize" {
		t.Fatalf("measurement fields = %+v, want only the keyed field", got.MeasurementFields)
	}
}

// The pen must ride on its OWN field, not only inside the display label. Packing left this column
// NULL, so anything filtering or grouping by pen missed every packing item.
func TestPackingItemCarriesPartitionAsAField(t *testing.T) {
	got := enqueuePacking(t, basePackingRequest())

	if got.PartitionLabel == nil || *got.PartitionLabel != "Part 2" {
		t.Fatalf("PartitionLabel = %v, want the pen as its own field", got.PartitionLabel)
	}
	if got.SubjectLabel == nil || *got.SubjectLabel != "Session 1 · Mandela 1 - Part 2" {
		t.Errorf("SubjectLabel = %v, want the session and the operational location", got.SubjectLabel)
	}
}

// The verifier's subject names the SESSION and the PEN (maintainer decision 2026-08-11, reverting the
// 2026-08-10 pen-only label). A pen produces TWO packing videos a day, so without the prefix a
// verifier holding both of Mandela 1 - Part 2's cards cannot tell which bag each clip proves.
//
// Asserted as an exact string, not a "contains the shed" check: the defect this replaces was a label
// that was present and correct-looking and still described the wrong scope.
func TestPackingSubjectNamesTheSessionAndThePen(t *testing.T) {
	got := enqueuePacking(t, basePackingRequest())

	if got.SubjectLabel == nil {
		t.Fatal("SubjectLabel is nil; the verifier's card would show no location at all")
	}
	const want = "Session 1 · Mandela 1 - Part 2"
	if *got.SubjectLabel != want {
		t.Fatalf("SubjectLabel = %q, want %q -- the session prefix, then shed and pen always together",
			*got.SubjectLabel, want)
	}
}

// The pen's TWO bags must be distinguishable in the queue. Same pen, same day, different session ->
// different label. This is the assertion the pen-day grain removed, and the reason it is a defect:
// two identical cards give the verifier no way to tell a crew that packed the morning share twice
// from one that packed both correctly.
func TestPackingSubjectsOfOnePensTwoBagsDiffer(t *testing.T) {
	morning := basePackingRequest()
	evening := basePackingRequest()
	evening.SessionNo = 2

	gotMorning := enqueuePacking(t, morning)
	gotEvening := enqueuePacking(t, evening)

	if gotMorning.SubjectLabel == nil || gotEvening.SubjectLabel == nil {
		t.Fatalf("labels = %v / %v, want both composed", gotMorning.SubjectLabel, gotEvening.SubjectLabel)
	}
	if *gotMorning.SubjectLabel == *gotEvening.SubjectLabel {
		t.Fatalf("both bags labelled %q; the verifier cannot tell them apart", *gotMorning.SubjectLabel)
	}
	if *gotEvening.SubjectLabel != "Session 2 · Mandela 1 - Part 2" {
		t.Errorf("evening label = %q, want the second session named", *gotEvening.SubjectLabel)
	}
}

// An UNDIVIDED shed renders with the session and the bare shed name -- no dangling separator, and
// never the matching key 'whole'.
func TestPackingSubjectOfAnUndividedShedIsTheSessionAndBareShedName(t *testing.T) {
	req := basePackingRequest()
	req.ShedName = "Yashoda"
	req.PartitionLabel = ""

	got := enqueuePacking(t, req)

	if got.SubjectLabel == nil || *got.SubjectLabel != "Session 1 · Yashoda" {
		t.Fatalf("SubjectLabel = %v, want %q", got.SubjectLabel, "Session 1 · Yashoda")
	}
	if got.PartitionLabel != nil {
		t.Errorf("PartitionLabel = %v, want nil for an undivided shed", got.PartitionLabel)
	}
}

// An UNRESOLVABLE location degrades to the bare session rather than a dangling separator or an empty
// card. This is the case migration 000150 step 4b repairs on rows the pen-day build had nulled: the
// session alone is what the producer composes, so it is what the migration restores.
func TestPackingSubjectDegradesToTheBareSessionWhenTheLocationIsUnresolvable(t *testing.T) {
	req := basePackingRequest()
	req.ShedName = ""
	req.PartitionLabel = ""

	got := enqueuePacking(t, req)

	if got.SubjectLabel == nil || *got.SubjectLabel != "Session 1" {
		t.Fatalf("SubjectLabel = %v, want a bare %q with no separator", got.SubjectLabel, "Session 1")
	}
}
