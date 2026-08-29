package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type fakeSaleRepo struct {
	rows []ports.SaleCandidateRow
	// readCalls counts how many times the confirm path re-read the herd, which is how
	// "the confirm never trusts the preview" is actually observable.
	readCalls int
	recorded  *ports.RecordSaleAllocationsCommand
}

func (f *fakeSaleRepo) ListSaleCandidates(context.Context, ports.ListSaleCandidatesParams) ([]ports.SaleCandidateRow, *string, error) {
	return f.rows, nil, nil
}

func (f *fakeSaleRepo) ReadSaleCandidateRows(_ context.Context, _, _ string, goatIDs []string) (map[string]ports.SaleCandidateRow, error) {
	f.readCalls++
	want := map[string]bool{}
	for _, id := range goatIDs {
		want[id] = true
	}
	out := map[string]ports.SaleCandidateRow{}
	for _, row := range f.rows {
		if want[row.GoatID] {
			out[row.GoatID] = row
		}
	}
	return out, nil
}

func (f *fakeSaleRepo) ListSaleAllocations(context.Context, string, string) ([]ports.SaleAllocationShedGroup, error) {
	return nil, nil
}

func (f *fakeSaleRepo) ListSaleLocations(context.Context, string) (*ports.SaleLocationCatalog, error) {
	return &ports.SaleLocationCatalog{}, nil
}

func (f *fakeSaleRepo) RecordSaleAllocations(_ context.Context, cmd ports.RecordSaleAllocationsCommand) (*ports.SaleAllocationResult, error) {
	f.recorded = &cmd
	return &ports.SaleAllocationResult{SalesDealID: cmd.SalesDealID, Allocated: len(cmd.Rows)}, nil
}

const (
	saleTenant = "11111111-1111-4111-8111-111111111111"
	saleActor  = "22222222-2222-4222-8222-222222222222"
	saleDeal   = "33333333-3333-4333-8333-333333333333"
	shedA      = "44444444-4444-4444-8444-444444444444"
	shedB      = "55555555-5555-4555-8555-555555555555"
)

func goatID(n int) string {
	return "66666666-6666-4666-8666-6666666666" + string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func candidate(n int, shedID, shedName, partition, tag, lifecycle string) ports.SaleCandidateRow {
	return ports.SaleCandidateRow{
		GoatID: goatID(n), DisplayID: "G-" + tag, TagNumber: tag,
		ParkName: "CPT", ShedID: shedID, ShedName: shedName, PartitionLabel: partition,
		State: domain.SaleCandidateState{
			GoatID: goatID(n), Exists: true, LifecycleStatus: lifecycle,
			ManagementStage: "F2", RowVersion: 4,
		},
	}
}

// fakeDeals states how many animals the sale is for. Defaults are set per test.
type fakeDeals struct {
	declared int
	tagged   int
	err      error
}

func (f *fakeDeals) ReadSaleDeal(context.Context, string, string) (*ports.SaleDeal, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ports.SaleDeal{DeclaredAnimalCount: f.declared, AlreadyTagged: f.tagged}, nil
}

func newSaleService(rows ...ports.SaleCandidateRow) (*SaleAllocationService, *fakeSaleRepo, *fakeDeals) {
	repo := &fakeSaleRepo{rows: rows}
	// Default: the deal is for exactly as many animals as the test seeded, so existing
	// tests exercise the happy path without each one restating the target.
	deals := &fakeDeals{declared: len(rows)}
	return NewSaleAllocationService(repo, repo, deals), repo, deals
}

// THE GATHER LIST. The person confirming reads the farm back to themselves shed by shed,
// because that is how they will physically walk out and collect the animals. A sale
// spanning two sheds must come back as two groups with the right animals in each.
func TestPreviewGroupsPickedAnimalsShedWise(t *testing.T) {
	svc, _, _ := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "alive"),
		candidate(3, shedB, "Gandhi", "2", "9053", "alive"),
	)
	preview, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal,
		GoatIDs: []string{goatID(1), goatID(2), goatID(3)},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Sellable != 3 || preview.Blocked != 0 {
		t.Fatalf("sellable=%d blocked=%d, want 3/0", preview.Sellable, preview.Blocked)
	}
	if len(preview.ShedGroups) != 2 {
		t.Fatalf("want two shed groups, got %d", len(preview.ShedGroups))
	}
	castro := preview.ShedGroups[0]
	if castro.OperationalLocationDisplay != "Castro 1" {
		t.Fatalf("numeric partition must render with a space: %q", castro.OperationalLocationDisplay)
	}
	if castro.Animals != 2 || strings.Join(castro.TagNumbers, ",") != "9051,9052" {
		t.Fatalf("Castro 1 group = %+v", castro)
	}
	if preview.ShedGroups[1].Animals != 1 {
		t.Fatalf("Gandhi 2 group = %+v", preview.ShedGroups[1])
	}
}

// A BLOCKED ANIMAL NEVER REACHES THE GATHER LIST. It is reported separately with its
// reason, and the two buckets are disjoint so the counts can be rendered side by side.
func TestPreviewKeepsBlockedAnimalsOffTheGatherList(t *testing.T) {
	svc, _, _ := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "quarantine"),
	)
	preview, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Sellable != 1 || preview.Blocked != 1 {
		t.Fatalf("sellable=%d blocked=%d, want 1/1", preview.Sellable, preview.Blocked)
	}
	if len(preview.ShedGroups) != 1 || preview.ShedGroups[0].Animals != 1 {
		t.Fatalf("the quarantined animal must not be on the gather list: %+v", preview.ShedGroups)
	}
	if preview.BlockedAnimals[0].BlockedReason != "In quarantine" {
		t.Fatalf("blocked reason = %q", preview.BlockedAnimals[0].BlockedReason)
	}
}

// FAIL-CLOSED, ALL-OR-NOTHING. One blocked animal refuses the WHOLE confirm and writes
// nothing. Applying the sellable ones and dropping the rest would leave the person
// believing they sold every animal they picked while the farm recorded fewer -- and the
// animals they physically loaded would still read as in-herd.
func TestConfirmRefusesEverythingWhenOneAnimalIsBlocked(t *testing.T) {
	svc, repo, _ := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "icu"),
	)
	_, err := svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-1",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err == nil {
		t.Fatal("a blocked animal must refuse the whole confirm")
	}
	if repo.recorded != nil {
		t.Fatalf("nothing may be written when the confirm is refused, got %+v", repo.recorded)
	}
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "animals_blocked" {
		t.Fatalf("error = %#v, want animals_blocked", err)
	}
	// The refusal must name the animal and say why, or the person has to go hunting.
	if !strings.Contains(appErr.Message, "9052") || !strings.Contains(appErr.Message, "In ICU") {
		t.Fatalf("refusal must name the animal and the reason, got %q", appErr.Message)
	}
	// And it must NOT leak the raw id of the animal.
	if strings.Contains(appErr.Message, goatID(2)) {
		t.Fatalf("refusal leaked a raw id to an operator: %q", appErr.Message)
	}
}

// THE CONFIRM RE-READS THE HERD. A preview is a snapshot; between it and the confirm a
// real animal can be put in quarantine by someone else. If the confirm trusted the
// preview's verdict it would sell that animal -- the exact state change the gate exists
// to catch.
func TestConfirmRejudgesInsteadOfTrustingThePreview(t *testing.T) {
	svc, repo, _ := newSaleService(candidate(1, shedA, "Castro", "1", "9051", "alive"))

	preview, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1)},
	})
	if err != nil || preview.Sellable != 1 {
		t.Fatalf("preview should have cleared the animal: %+v %v", preview, err)
	}

	// The world changes after the preview: the animal goes into quarantine.
	repo.rows = []ports.SaleCandidateRow{candidate(1, shedA, "Castro", "1", "9051", "quarantine")}

	if _, err := svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-2",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1)},
	}); err == nil {
		t.Fatal("the confirm must re-judge and refuse an animal quarantined after the preview")
	}
	if repo.readCalls < 2 {
		t.Fatalf("confirm must re-read the herd, saw %d read(s) across preview+confirm", repo.readCalls)
	}
	if repo.recorded != nil {
		t.Fatal("nothing may be written")
	}
}

// A clean confirm carries every picked animal AND its captured row_version, which is what
// makes the write no-clobber downstream.
func TestConfirmCarriesEveryAnimalWithItsCapturedRowVersion(t *testing.T) {
	svc, repo, _ := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedB, "Gandhi", "2", "9052", "alive"),
	)
	result, err := svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-3",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result.Allocated != 2 {
		t.Fatalf("allocated = %d, want 2", result.Allocated)
	}
	if len(repo.recorded.Rows) != 2 {
		t.Fatalf("recorded rows = %+v", repo.recorded.Rows)
	}
	for _, row := range repo.recorded.Rows {
		if row.RowVersion != 4 {
			t.Fatalf("row %s lost its captured row_version: %+v", row.GoatID, row)
		}
	}
	// The stored idempotency key must be scoped to the deal, so the same client key
	// against a different sale is a different write rather than a silent replay.
	if !strings.Contains(repo.recorded.StoredIdempotencyKey, saleDeal) {
		t.Fatalf("stored idempotency key must be deal-scoped, got %q", repo.recorded.StoredIdempotencyKey)
	}
}

// A client that sends the same animal twice means it once. Letting the duplicate through
// would double the head count on the review screen and try to tag one animal twice.
func TestDuplicatePicksCollapseToOneAnimal(t *testing.T) {
	svc, repo, _ := newSaleService(candidate(1, shedA, "Castro", "1", "9051", "alive"))
	result, err := svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-4",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(1), goatID(1)},
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result.Allocated != 1 || len(repo.recorded.Rows) != 1 {
		t.Fatalf("duplicates must collapse, got %d", result.Allocated)
	}
}

// An animal the read did not return is a MISSING animal, reported as such. Dropping it
// silently would show a person who picked 2 a review of 1 with no explanation.
func TestAnAnimalThatVanishedIsReportedNotDropped(t *testing.T) {
	svc, _, _ := newSaleService(candidate(1, shedA, "Castro", "1", "9051", "alive"))
	preview, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Sellable+preview.Blocked != 2 {
		t.Fatalf("both picked animals must be accounted for, got %d+%d", preview.Sellable, preview.Blocked)
	}
	if preview.BlockedAnimals[0].BlockedReason != "This animal is not in the herd" {
		t.Fatalf("missing animal reason = %q", preview.BlockedAnimals[0].BlockedReason)
	}
}

// There is deliberately no override, and the cap keeps one confirm bounded.
func TestConfirmRefusesAnUnboundedPayload(t *testing.T) {
	svc, _, _ := newSaleService()
	ids := make([]string, 0, ports.MaxSaleAllocationGoatsPerCommand+1)
	for i := 0; i < ports.MaxSaleAllocationGoatsPerCommand+1; i++ {
		ids = append(ids, goatID(i%100))
	}
	_, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: ids,
	})
	// The ids repeat, so dedup brings them under the cap -- that is the point: dedup runs
	// BEFORE the cap, so a sloppy client is not punished for duplicates.
	if err != nil {
		t.Fatalf("deduplicated ids must fit under the cap: %v", err)
	}
}

// THE COUNT GATE (maintainer rule 2026-08-20). A sale is for a stated number of animals
// and the mapping must hit it EXACTLY. Half now and half later is refused outright:
// a part-mapped sale leaves the ledger saying one thing and the herd another, with no
// screen showing the gap.
func TestASaleMustBeMappedExactlyAndNeverInHalves(t *testing.T) {
	rows := []ports.SaleCandidateRow{
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "alive"),
		candidate(3, shedB, "Gandhi", "2", "9053", "alive"),
	}
	confirm := func(deal *fakeDeals, ids []string) error {
		repo := &fakeSaleRepo{rows: rows}
		svc := NewSaleAllocationService(repo, repo, deal)
		_, err := svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
			TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "count-gate-key",
			SalesDealID: saleDeal, GoatIDs: ids,
		})
		return err
	}
	all := []string{goatID(1), goatID(2), goatID(3)}

	// TOO FEW: the sale is for 3, two are picked.
	err := confirm(&fakeDeals{declared: 3}, all[:2])
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "sale_not_fully_mapped" {
		t.Fatalf("a half-mapped sale must be refused, got %#v", err)
	}
	if !strings.Contains(appErr.Message, "3") {
		t.Fatalf("the refusal must name the target so the operator knows what to pick: %q", appErr.Message)
	}

	// TOO MANY: the sale is for 2, three are picked. This is the defect that shipped --
	// six animals were mapped to a five-animal sale with no complaint.
	err = confirm(&fakeDeals{declared: 2}, all)
	if appErr, ok = err.(*Error); !ok || appErr.Code != "sale_over_mapped" {
		t.Fatalf("an over-mapped sale must be refused, got %#v", err)
	}

	// ALREADY COMPLETE: every animal is tagged, so this deal is finished.
	err = confirm(&fakeDeals{declared: 3, tagged: 3}, all)
	if appErr, ok = err.(*Error); !ok || appErr.Code != "sale_already_mapped" {
		t.Fatalf("a finished sale must be refused, got %#v", err)
	}

	// EXACT: the sale is for 3 and 3 are picked.
	if err := confirm(&fakeDeals{declared: 3}, all); err != nil {
		t.Fatalf("an exact fill must be accepted: %v", err)
	}

	// EXACT AGAINST A PART-FILLED SALE: 3 declared, 1 already tagged, 2 picked now.
	// The remainder is what must be hit -- not the whole declared count -- or a sale that
	// was ever partly mapped could never be completed.
	if err := confirm(&fakeDeals{declared: 3, tagged: 1}, all[:2]); err != nil {
		t.Fatalf("filling the remainder must be accepted: %v", err)
	}
}

// The preview carries the target, so the screen can show "2 of 3" and offer Confirm only
// when the mapping is exact. The same arithmetic decides both -- the client never forms
// its own opinion about a server-side gate.
func TestPreviewReportsTheTargetAndWhetherTheSaleIsComplete(t *testing.T) {
	repo := &fakeSaleRepo{rows: []ports.SaleCandidateRow{
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "alive"),
	}}
	svc := NewSaleAllocationService(repo, repo, &fakeDeals{declared: 3})

	partial, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if partial.DeclaredAnimalCount != 3 {
		t.Fatalf("preview must carry the sale's target, got %d", partial.DeclaredAnimalCount)
	}
	if partial.Complete {
		t.Fatal("2 of 3 is not complete and must not offer Confirm")
	}

	// A blocked animal makes the mapping incomplete even when the COUNT matches: the
	// blocked one cannot be sold, so the sale would be short by one.
	blockedRepo := &fakeSaleRepo{rows: []ports.SaleCandidateRow{
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "quarantine"),
	}}
	svc = NewSaleAllocationService(blockedRepo, blockedRepo, &fakeDeals{declared: 2})
	withBlocked, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if withBlocked.Complete {
		t.Fatal("a picked-but-blocked animal must not count toward a complete mapping")
	}
}
