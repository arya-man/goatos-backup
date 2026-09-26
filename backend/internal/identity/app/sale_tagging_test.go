package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// The tag-only Sales surface (maintainer decision 2026-09-11): a park head tags animals to a
// sale from THEIR park. These tests pin the two server-side halves of that -- the park clamp
// on every allocation step and the tag-only queue -- with the fakes, on the same
// service the production handler calls.

const (
	parkA = "77777777-7777-4777-8777-777777777771"
	parkB = "77777777-7777-4777-8777-777777777772"
)

func candidateInPark(n int, parkID, tag string) ports.SaleCandidateRow {
	row := candidate(n, shedA, "Castro", "1", tag, "alive")
	row.ParkID = parkID
	return row
}

// A park-scoped caller may confirm animals from their own park and from nowhere else. The
// refusal is a 403 with its own code, writes nothing, and comes BEFORE the count gate so a
// crafted request cannot learn how many animals a sale at another park still needs.
func TestConfirmRefusesAnimalsOutsideTheCallersParkScope(t *testing.T) {
	svc, repo, _ := newSaleService(
		candidateInPark(1, parkA, "9051"),
		candidateInPark(2, parkA, "9052"),
	)
	// The sale is recorded at park A (fakeDeals' default farm is CPT), so park A's scope owns it.
	repo.catalog = &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{{ParkID: parkA, Label: "CPT"}, {ParkID: parkB, Label: "CBE"}}}
	base := ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-scope",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
		AnimalWeightsKg: weightsFor([]string{goatID(1), goatID(2)}),
	}

	in := base
	in.AllowedParkIDs = []string{parkB}
	_, err := svc.ConfirmSaleAllocation(context.Background(), in)
	appErr, ok := err.(*Error)
	if !ok || appErr.Code != "park_out_of_scope" || appErr.HTTPStatus != 403 {
		t.Fatalf("confirm from another park: err = %#v, want 403 park_out_of_scope", err)
	}
	if repo.recorded != nil {
		t.Fatalf("nothing may be written when the park is out of scope, got %+v", repo.recorded)
	}

	in = base
	in.AllowedParkIDs = []string{parkA}
	if _, err := svc.ConfirmSaleAllocation(context.Background(), in); err != nil {
		t.Fatalf("confirm within the caller's own park: %v", err)
	}
	if repo.recorded == nil || len(repo.recorded.Rows) != 2 {
		t.Fatalf("the in-scope confirm must be written, got %+v", repo.recorded)
	}

	// nil is tenant-wide: the sales desk and the CXO see every park, exactly as before.
	repo.recorded = nil
	in = base
	in.IdempotencyKey = "confirm-scope-2"
	if _, err := svc.ConfirmSaleAllocation(context.Background(), in); err != nil {
		t.Fatalf("tenant-wide confirm: %v", err)
	}
}

// The same clamp on the review and on the picker, so a park head never even SEES another
// park's animals -- the refusal is at the first read, not only at the write.
func TestPreviewAndPickerRefuseAnotherPark(t *testing.T) {
	svc, _, _ := newSaleService(candidateInPark(1, parkA, "9051"))

	_, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1)}, AllowedParkIDs: []string{parkB},
	})
	if appErr, ok := err.(*Error); !ok || appErr.Code != "park_out_of_scope" {
		t.Fatalf("preview from another park: err = %#v, want park_out_of_scope", err)
	}

	_, _, err = svc.ListSaleCandidates(context.Background(), ListSaleCandidatesInput{
		TenantID: saleTenant, ParkID: parkA, AllowedParkIDs: []string{parkB},
	})
	if appErr, ok := err.(*Error); !ok || appErr.Code != "park_out_of_scope" {
		t.Fatalf("picker for another park: err = %#v, want park_out_of_scope", err)
	}
	if _, _, err := svc.ListSaleCandidates(context.Background(), ListSaleCandidatesInput{
		TenantID: saleTenant, ParkID: parkA, AllowedParkIDs: []string{parkA},
	}); err != nil {
		t.Fatalf("picker for the caller's own park: %v", err)
	}
}

// The queue is narrowed to the FARM CODES of the caller's parks (the ledger names a sale's farm
// by code, never by park id) and FAILS CLOSED: a park scope that resolves to no farm sees an
// empty queue, never every farm's. A tenant-wide caller (nil scope) asks with no filter.
func TestTaggingQueueNarrowsToTheCallersFarmsAndFailsClosed(t *testing.T) {
	svc, repo, deals := newSaleService()
	repo.catalog = &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{
		{ParkID: parkA, Label: "CPT"},
		{ParkID: parkB, Label: "CBE"},
	}}
	deals.queue = []ports.SaleTaggingDeal{{SalesDealID: saleDeal, Farm: "CPT", DeclaredAnimalCount: 12, AlreadyTagged: 8}}

	out, _, err := svc.ListSaleTaggingQueue(context.Background(), ListSaleTaggingQueueInput{TenantID: saleTenant, AllowedParkIDs: []string{parkA}})
	if err != nil {
		t.Fatalf("park-scoped queue: %v", err)
	}
	if len(deals.queueCalls) != 1 || len(deals.queueCalls[0]) != 1 || deals.queueCalls[0][0] != "CPT" {
		t.Fatalf("queue asked with farms %v, want [CPT] for park A", deals.queueCalls)
	}
	if len(out) != 1 || out[0].Remaining() != 4 {
		t.Fatalf("queue = %+v, want the CPT sale with 4 remaining", out)
	}

	deals.queueCalls, deals.queueFarmsSeen = nil, false
	out, _, err = svc.ListSaleTaggingQueue(context.Background(), ListSaleTaggingQueueInput{TenantID: saleTenant, AllowedParkIDs: []string{"not-a-park"}})
	if err != nil || len(out) != 0 {
		t.Fatalf("unknown park scope: out=%+v err=%v, want an empty queue", out, err)
	}
	if deals.queueFarmsSeen {
		t.Fatal("a scope resolving to no farm must not read the ledger at all")
	}

	deals.queueCalls, deals.queueFarmsSeen = nil, false
	if _, _, err := svc.ListSaleTaggingQueue(context.Background(), ListSaleTaggingQueueInput{TenantID: saleTenant}); err != nil {
		t.Fatalf("tenant-wide queue: %v", err)
	}
	if !deals.queueFarmsSeen || deals.queueCalls[0] != nil {
		t.Fatalf("tenant-wide queue must ask with no farm filter, got %v", deals.queueCalls)
	}
}

// THE SALE'S OWN FARM IS IN SCOPE TOO. Clamping the animals alone let a park head tag animals
// from their own pen onto a sale another park recorded, and read back any sale's tags and weights
// by id. A park-scoped caller is refused a sale whose farm is not one of their parks'
// codes on the review, the confirm and the read-back -- before anything is read about animals or
// written. Tenant-wide callers (nil scope) are untouched.
func TestPreviewConfirmAndReadBackRefuseASaleAtAnotherPark(t *testing.T) {
	svc, repo, deals := newSaleService(candidateInPark(1, parkA, "9051"))
	repo.catalog = &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{
		{ParkID: parkA, Label: "CPT"},
		{ParkID: parkB, Label: "CBE"},
	}}
	deals.farm = "CBE" // the sale was recorded at park B

	_, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1)}, AllowedParkIDs: []string{parkA},
	})
	if appErr, ok := err.(*Error); !ok || appErr.Code != "park_out_of_scope" || appErr.HTTPStatus != 403 {
		t.Fatalf("preview of another park's sale: err = %#v, want 403 park_out_of_scope", err)
	}

	_, err = svc.ConfirmSaleAllocation(context.Background(), ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-farm",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1)},
		AnimalWeightsKg: weightsFor([]string{goatID(1)}), AllowedParkIDs: []string{parkA},
	})
	if appErr, ok := err.(*Error); !ok || appErr.Code != "park_out_of_scope" {
		t.Fatalf("confirm onto another park's sale: err = %#v, want park_out_of_scope", err)
	}
	if repo.recorded != nil {
		t.Fatalf("nothing may be written for another park's sale, got %+v", repo.recorded)
	}

	if _, err := svc.GetSaleAllocationAnimals(context.Background(), saleTenant, saleDeal, []string{parkA}); err == nil {
		t.Fatal("read-back of another park's sale must be refused")
	}
	if _, err := svc.GetSaleAllocation(context.Background(), saleTenant, saleDeal, []string{parkA}); err == nil {
		t.Fatal("grouped read-back of another park's sale must be refused")
	}

	// The same sale is the caller's own when their scope includes park B, and every step runs.
	deals.farm = "CPT"
	if _, err := svc.PreviewSaleAllocation(context.Background(), PreviewSaleAllocationInput{
		TenantID: saleTenant, SalesDealID: saleDeal, GoatIDs: []string{goatID(1)}, AllowedParkIDs: []string{parkA},
	}); err != nil {
		t.Fatalf("preview of the caller's own sale: %v", err)
	}
	if _, err := svc.GetSaleAllocationAnimals(context.Background(), saleTenant, saleDeal, []string{parkA}); err != nil {
		t.Fatalf("read-back of the caller's own sale: %v", err)
	}

	// Tenant-wide: any farm, and the farm is never even looked up.
	deals.farm, deals.farmReads = "CBE", 0
	if _, err := svc.GetSaleAllocationAnimals(context.Background(), saleTenant, saleDeal, nil); err != nil {
		t.Fatalf("tenant-wide read-back: %v", err)
	}
	if deals.farmReads != 0 {
		t.Fatalf("a tenant-wide caller must not pay for a farm lookup, got %d reads", deals.farmReads)
	}
}

// The park/pen vocabulary a park-scoped caller is served names their own parks only, so the
// phone never offers a park the server would then refuse.
func TestSaleLocationsNarrowToTheCallersParks(t *testing.T) {
	svc, repo, _ := newSaleService()
	repo.catalog = &ports.SaleLocationCatalog{
		Parks: []ports.SaleLocationPark{{ParkID: parkA, Label: "CPT"}, {ParkID: parkB, Label: "CBE"}},
		Locations: []ports.SaleLocationEntry{
			{ShedID: shedA, ParkID: parkA, OperationalLocationDisplay: "Castro 1"},
			{ShedID: shedB, ParkID: parkB, OperationalLocationDisplay: "Gandhi 2"},
		},
	}
	got, err := svc.GetSaleLocations(context.Background(), saleTenant, []string{parkA})
	if err != nil {
		t.Fatalf("scoped locations: %v", err)
	}
	if len(got.Parks) != 1 || got.Parks[0].ParkID != parkA || len(got.Locations) != 1 || got.Locations[0].ParkID != parkA {
		t.Fatalf("scoped catalog = %+v, want park A only", got)
	}
	all, err := svc.GetSaleLocations(context.Background(), saleTenant, nil)
	if err != nil || len(all.Parks) != 2 || len(all.Locations) != 2 {
		t.Fatalf("tenant-wide catalog = %+v err=%v, want both parks", all, err)
	}
}

// AN EXACT REPLAY RETURNS THE ORIGINAL RESULT. A phone that lost the reply retries the same
// confirm with the same Idempotency-Key AFTER the server tagged the sale: the animals now read
// "Already sold" and the sale reads complete, so re-judging them would refuse the retry the caller
// is entitled to. The replay is answered from the idempotency ledger BEFORE the herd is re-read,
// writes nothing, and a same-key request with a DIFFERENT body is refused rather than replayed.
func TestAnExactReplayOfACommittedConfirmReturnsTheOriginalResult(t *testing.T) {
	svc, repo, deals := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedA, "Castro", "1", "9052", "alive"),
	)
	ids := []string{goatID(1), goatID(2)}
	in := ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-replay-1",
		SalesDealID: saleDeal, GoatIDs: ids, AnimalWeightsKg: map[string]string{goatID(1): "32.4", goatID(2): "29"},
	}
	first, err := svc.ConfirmSaleAllocation(context.Background(), in)
	if err != nil || first.Allocated != 2 {
		t.Fatalf("first confirm: %+v %v", first, err)
	}
	if repo.recorded.RequestHash == "" {
		t.Fatal("the confirm must store a request fingerprint beside its key")
	}

	// The world after the first confirm: both animals sold, the sale full.
	for i := range repo.rows {
		repo.rows[i].State.LifecycleStatus = "sold"
	}
	deals.tagged = 2
	readsBefore, writesBefore := repo.readCalls, repo.recordCalls

	// Same key, same body -- in a different order, with the weight typed the same way.
	replay := in
	replay.GoatIDs = []string{goatID(2), goatID(1)}
	again, err := svc.ConfirmSaleAllocation(context.Background(), replay)
	if err != nil {
		t.Fatalf("exact replay was refused: %v", err)
	}
	if again.Allocated != first.Allocated || again.SalesDealID != first.SalesDealID {
		t.Fatalf("replay = %+v, want the original %+v", again, first)
	}
	if repo.readCalls != readsBefore || repo.recordCalls != writesBefore {
		t.Fatalf("a replay must not re-judge or write: reads %d->%d writes %d->%d", readsBefore, repo.readCalls, writesBefore, repo.recordCalls)
	}

	// Same key, different weight: not the same request, so it is refused, never replayed.
	changed := in
	changed.AnimalWeightsKg = map[string]string{goatID(1): "40", goatID(2): "29"}
	_, err = svc.ConfirmSaleAllocation(context.Background(), changed)
	if appErr, ok := err.(*Error); !ok || appErr.Code != "idempotency_conflict" {
		t.Fatalf("same key, different body: err = %#v, want idempotency_conflict", err)
	}

	// A NEW key for the same animals is new work, judged as such: they are sold now.
	fresh := in
	fresh.IdempotencyKey = "confirm-replay-2"
	_, err = svc.ConfirmSaleAllocation(context.Background(), fresh)
	if appErr, ok := err.(*Error); !ok || (appErr.Code != "animals_blocked" && appErr.Code != "sale_already_mapped") {
		t.Fatalf("new key after the sale is full: err = %#v, want a refusal", err)
	}
}

// The replay still sits behind the park clamp: a key cannot be used to read another park's sale.
func TestAReplayIsStillClampedToTheCallersPark(t *testing.T) {
	svc, repo, deals := newSaleService(candidateInPark(1, parkA, "9051"))
	repo.catalog = &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{{ParkID: parkA, Label: "CPT"}, {ParkID: parkB, Label: "CBE"}}}
	in := ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-replay-3",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1)}, AnimalWeightsKg: weightsFor([]string{goatID(1)}),
	}
	if _, err := svc.ConfirmSaleAllocation(context.Background(), in); err != nil {
		t.Fatalf("tenant-wide confirm: %v", err)
	}
	deals.farm = "CBE"
	in.AllowedParkIDs = []string{parkA}
	_, err := svc.ConfirmSaleAllocation(context.Background(), in)
	if appErr, ok := err.(*Error); !ok || appErr.Code != "park_out_of_scope" {
		t.Fatalf("replay from another park: err = %#v, want park_out_of_scope", err)
	}
}
