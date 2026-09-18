package domain

import (
	"testing"
	"time"
)

func fbFact(id, bucket, breed, species, sex, stage, shed, shedName, partition, park, saleDate string, value *float64) FarmBornAnimalFact {
	return FarmBornAnimalFact{
		GoatID: id, DisplayID: "G-" + id, Tag: "TAG" + id,
		Species: species, Breed: breed, Sex: sex, Stage: stage,
		ParkID: park, ParkName: "Park " + park, ShedID: shed, ShedName: shedName, PartitionLabel: partition,
		Bucket: bucket, SaleDate: saleDate, SaleValue: value, BuyerName: "Buyer", DealID: "deal",
	}
}

func f64(v float64) *float64 { return &v }

// TestBuildFarmBornSalesBreakdownsSumToTheHeadline pins the grain rule: every breakdown ranges
// over the same animals as the summary, so its On farm and Sold columns sum to the headline, an
// unpriced sale counts as sold but earns nothing, and the average divides only over priced sales.
func TestBuildFarmBornSalesBreakdownsSumToTheHeadline(t *testing.T) {
	facts := []FarmBornAnimalFact{
		fbFact("1", FarmBornOnFarm, "Sirohi", "goat", "female", "F2-Female", "s1", "Castro", "1", "p1", "", nil),
		fbFact("2", FarmBornOnFarm, "Sirohi", "goat", "male", "F2-Male", "s1", "Castro", "2", "p1", "", nil),
		fbFact("3", FarmBornOnFarm, "", "sheep", "female", "", "s2", "Yashoda", "", "p2", "", nil),
		fbFact("4", FarmBornSold, "Sirohi", "goat", "male", "F2-Male", "s1", "Castro", "2", "p1", "2026-09-10", f64(9000)),
		fbFact("5", FarmBornSold, "Sirohi", "goat", "male", "F2-Male", "s1", "Castro", "2", "p1", "2026-09-12", f64(11000)),
		// Sold without a deal: counted, unpriced.
		fbFact("6", FarmBornSold, "Boer", "goat", "female", "F2-Female", "s2", "Yashoda", "", "p2", "2026-09-01", nil),
	}
	filter := FarmBornFilter{From: "2026-08-18", To: "2026-09-18"}
	out := BuildFarmBornSales(facts, filter, 25, 0)

	s := out.Summary
	if s.OnFarm != 3 || s.Sold != 3 || s.SoldPriced != 2 || s.Revenue != 20000 || s.AvgPrice != 10000 {
		t.Fatalf("summary = %+v", s)
	}
	if s.From != filter.From || s.To != filter.To {
		t.Fatalf("summary window = %+v", s)
	}
	for name, buckets := range map[string][]FarmBornBucket{"breed": out.ByBreed, "sex": out.BySex, "stage": out.ByStage, "pen": out.ByPen} {
		onFarm, sold, priced, revenue := 0, 0, 0, 0.0
		for _, b := range buckets {
			onFarm += b.OnFarm
			sold += b.Sold
			priced += b.SoldPriced
			revenue += b.Revenue
		}
		if onFarm != s.OnFarm || sold != s.Sold || priced != s.SoldPriced || revenue != s.Revenue {
			t.Fatalf("%s breakdown sums (%d/%d/%d/%v) disagree with the headline (%d/%d/%d/%v)", name, onFarm, sold, priced, revenue, s.OnFarm, s.Sold, s.SoldPriced, s.Revenue)
		}
	}

	// Most-sold first: Sirohi (2 sold) leads Boer (1 sold), and the blank breed reads as a fact.
	if len(out.ByBreed) != 3 || out.ByBreed[0].Label != "Sirohi" || out.ByBreed[1].Label != "Boer" || out.ByBreed[2].Label != FarmBornUnknownBreed {
		t.Fatalf("by breed = %+v", out.ByBreed)
	}
	if out.ByBreed[0].Detail != "Goat" || out.ByBreed[2].Detail != "Sheep" {
		t.Fatalf("breed detail must name the species: %+v", out.ByBreed)
	}
	// Sex labels are the backend's, never the raw register value.
	if out.BySex[0].Label != "Male" || out.BySex[0].Sold != 2 || out.BySex[1].Label != "Female" {
		t.Fatalf("by sex = %+v", out.BySex)
	}
	if out.ByStage[len(out.ByStage)-1].Label != FarmBornUnknownStage {
		t.Fatalf("blank stage must read as not recorded: %+v", out.ByStage)
	}
	// Pens: Castro 2 (2 sold), Yashoda (1 sold), Castro 1 (0 sold); the pen label is the farm's
	// spelling and the park rides as detail so same-named pens in two parks stay apart.
	if len(out.ByPen) != 3 || out.ByPen[0].Label != "Castro 2" || out.ByPen[0].Key != "s1|2" || out.ByPen[0].ParkID != "p1" || out.ByPen[0].Detail != "Park p1" {
		t.Fatalf("by pen = %+v", out.ByPen)
	}
	if out.ByPen[1].Label != "Yashoda" || out.ByPen[1].Key != "s2" || out.ByPen[2].Label != "Castro 1" {
		t.Fatalf("by pen order = %+v", out.ByPen)
	}

	// The ledger: newest sale first, whole-filter total, the unpriced row carries no value.
	if out.TotalSold != 3 || len(out.Sold) != 3 || out.Sold[0].GoatID != "5" || out.Sold[1].GoatID != "4" || out.Sold[2].GoatID != "6" {
		t.Fatalf("sold ledger = %+v", out.Sold)
	}
	if out.Sold[2].SaleValue != nil || out.Sold[0].SaleValue == nil || *out.Sold[0].SaleValue != 11000 {
		t.Fatalf("sold values = %+v", out.Sold)
	}
	if out.Sold[0].PenDisplay != "Castro 2" || out.Sold[2].PenDisplay != "Yashoda" {
		t.Fatalf("sold pens = %q / %q", out.Sold[0].PenDisplay, out.Sold[2].PenDisplay)
	}
}

// TestBuildFarmBornSalesPagesTheLedgerOnly pins that Offset/Limit slice the ledger and nothing
// else: the summary and the breakdowns are whole-filter whatever page is asked for.
func TestBuildFarmBornSalesPagesTheLedgerOnly(t *testing.T) {
	facts := []FarmBornAnimalFact{}
	for i := 0; i < 7; i++ {
		facts = append(facts, fbFact(string(rune('a'+i)), FarmBornSold, "Sirohi", "goat", "male", "F2-Male", "s1", "Castro", "", "p1", "2026-09-0"+string(rune('1'+i)), f64(1000)))
	}
	out := BuildFarmBornSales(facts, FarmBornFilter{}, 3, 3)
	if out.TotalSold != 7 || len(out.Sold) != 3 || out.Limit != 3 || out.Offset != 3 {
		t.Fatalf("page = total %d rows %d limit %d offset %d", out.TotalSold, len(out.Sold), out.Limit, out.Offset)
	}
	if out.Summary.Sold != 7 || out.Summary.Revenue != 7000 || out.ByBreed[0].Sold != 7 {
		t.Fatalf("summary must be whole-filter: %+v / %+v", out.Summary, out.ByBreed)
	}
	past := BuildFarmBornSales(facts, FarmBornFilter{}, 3, 30)
	if len(past.Sold) != 0 || past.TotalSold != 7 {
		t.Fatalf("offset past the end = %+v", past)
	}
	if got := ClampFarmBornPageSize(0); got != DefaultFarmBornPageSize {
		t.Fatalf("clamp 0 = %d", got)
	}
	if got := ClampFarmBornPageSize(MaxFarmBornPageSize + 1); got != MaxFarmBornPageSize {
		t.Fatalf("clamp over = %d", got)
	}
}

// TestFarmBornPenKeyRoundTrips pins the pen filter value: shed id alone for an undivided pen (and
// for the 'whole' sentinel, which must never leak into a key), shed|label for a partition.
func TestFarmBornPenKeyRoundTrips(t *testing.T) {
	for _, tc := range []struct{ shed, partition, key string }{
		{"s1", "", "s1"},
		{"s1", "whole", "s1"},
		{"s1", "Part 3", "s1|Part 3"},
		{"s1", "2", "s1|2"},
		{"", "2", ""},
	} {
		if got := FarmBornPenKey(tc.shed, tc.partition); got != tc.key {
			t.Fatalf("key(%q,%q) = %q, want %q", tc.shed, tc.partition, got, tc.key)
		}
	}
	if shed, part := SplitFarmBornPenKey("s1|Part 3"); shed != "s1" || part != "Part 3" {
		t.Fatalf("split = %q / %q", shed, part)
	}
	if shed, part := SplitFarmBornPenKey("s1"); shed != "s1" || part != "" {
		t.Fatalf("split bare = %q / %q", shed, part)
	}
}

// TestDefaultFarmBornWindowIsTheLastMonth pins "last one month": a calendar month back, ending
// today, and clamped by the calendar when the month before is shorter.
func TestDefaultFarmBornWindowIsTheLastMonth(t *testing.T) {
	from, to := DefaultFarmBornWindow(time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC))
	if from != "2026-08-18" || to != "2026-09-18" {
		t.Fatalf("window = %s..%s", from, to)
	}
	from, to = DefaultFarmBornWindow(time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
	if from != "2026-03-03" || to != "2026-03-31" {
		// Go's AddDate normalises 31 Feb forward, which is the accepted reading.
		t.Fatalf("window = %s..%s", from, to)
	}
}
