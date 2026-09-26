package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// The tag-only Sales surface (maintainer decision 2026-09-11): a park head tags animals to a
// sale from THEIR park. These tests pin the two server-side halves of that -- the park clamp
// on every allocation step, and the optional rate per animal -- with the fakes, on the same
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

// The rate is OPTIONAL on the wire (the web drawer sends none) and carried per animal when
// present. A malformed rate, or one for an animal not being tagged, refuses the whole confirm
// and writes nothing -- a rate silently dropped would read to the sales desk as "no price
// agreed" for an animal the park head priced.
func TestConfirmCarriesAnOptionalRatePerAnimal(t *testing.T) {
	svc, repo, _ := newSaleService(
		candidate(1, shedA, "Castro", "1", "9051", "alive"),
		candidate(2, shedB, "Gandhi", "2", "9052", "alive"),
	)
	base := ConfirmSaleAllocationInput{
		TenantID: saleTenant, ActorID: saleActor, IdempotencyKey: "confirm-r",
		SalesDealID: saleDeal, GoatIDs: []string{goatID(1), goatID(2)},
		AnimalWeightsKg: weightsFor([]string{goatID(1), goatID(2)}),
	}
	for name, tc := range map[string]struct {
		rates map[string]string
		code  string
	}{
		"zero":           {map[string]string{goatID(1): "0"}, "invalid_rate"},
		"negative":       {map[string]string{goatID(1): "-5000"}, "invalid_rate"},
		"not a number":   {map[string]string{goatID(1): "five thousand"}, "invalid_rate"},
		"three decimals": {map[string]string{goatID(1): "5000.125"}, "invalid_rate"},
		"unknown animal": {map[string]string{goatID(3): "5000"}, "rate_for_unknown_animal"},
	} {
		in := base
		in.AnimalRatesRupees = tc.rates
		_, err := svc.ConfirmSaleAllocation(context.Background(), in)
		appErr, ok := err.(*Error)
		if !ok || appErr.Code != tc.code {
			t.Fatalf("%s: err = %#v, want %s", name, err, tc.code)
		}
		if repo.recorded != nil {
			t.Fatalf("%s: nothing may be written, got %+v", name, repo.recorded)
		}
	}

	// Absent rates: the confirm is valid and every row carries "" (stored as NULL).
	if _, err := svc.ConfirmSaleAllocation(context.Background(), base); err != nil {
		t.Fatalf("confirm without rates: %v", err)
	}
	for _, row := range repo.recorded.Rows {
		if row.RateRupees != "" {
			t.Fatalf("row %s rate = %q, want blank when none was sent", row.GoatID, row.RateRupees)
		}
	}

	// Present rates ride each row by goat id, trimmed; a blank entry means "none" for that one.
	repo.recorded = nil
	in := base
	in.IdempotencyKey = "confirm-r2"
	in.AnimalRatesRupees = map[string]string{goatID(1): " 5200.50 ", goatID(2): ""}
	if _, err := svc.ConfirmSaleAllocation(context.Background(), in); err != nil {
		t.Fatalf("confirm with rates: %v", err)
	}
	want := map[string]string{goatID(1): "5200.50", goatID(2): ""}
	for _, row := range repo.recorded.Rows {
		if row.RateRupees != want[row.GoatID] {
			t.Fatalf("row %s rate = %q want %q", row.GoatID, row.RateRupees, want[row.GoatID])
		}
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
// from their own pen onto a sale another park recorded, and read back any sale's tags, weights
// and rates by id. A park-scoped caller is refused a sale whose farm is not one of their parks'
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
