package domain

import "testing"

func TestSaleWeightAggregatesRespectQuantityUnits(t *testing.T) {
	for _, code := range []string{"sheep_tags", ProductCodeManure} {
		t.Run(code, func(t *testing.T) {
			cases := []struct {
				name, unit       string
				quantity, weight *float64
				want             float64
			}{
				{"counted", UnitNumber, fp(100), nil, 0},
				{"counted_with_recorded_weight", UnitNumber, fp(100), fp(2), 2},
				{"kilograms", UnitKg, fp(100), nil, 100},
				{"legacy_weight", "", nil, fp(20), 20},
				{"zero_kg_is_recorded", UnitKg, fp(0), fp(20), 0},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					line := DealLine{ProductType: "Authored item", ProductCode: code, ProductKind: KindOther, Unit: tc.unit, Quantity: tc.quantity, TotalWeightKg: tc.weight, SalesValue: 1000}
					summary, monthly, _, _ := BuildDealAggregates([]Deal{{SaleDate: "2026-09-25", SalesValue: 1000, Lines: []DealLine{line}}})
					got, month := summary.OtherKg, monthly[0].OtherKg
					revenue := summary.OtherRevenue
					if code == ProductCodeManure {
						got, month, revenue = summary.ManureKg, monthly[0].ManureKg, summary.ManureRevenue
					}
					if got != tc.want || month != tc.want {
						t.Fatalf("weight summary/month=%v/%v want %v", got, month, tc.want)
					}
					if revenue != 1000 || summary.Revenue != 1000 {
						t.Fatalf("revenue changed: %+v", summary)
					}
				})
			}
		})
	}
}
