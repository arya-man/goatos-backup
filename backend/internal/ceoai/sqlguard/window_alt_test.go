package sqlguard

import "testing"

type altDateCard struct {
	fakeCard
	alts []string
}

func (c altDateCard) AlternateDateColumns() []string { return c.alts }

// A period may be bound on a declared alternate date column (loads PURCHASED
// in a period bind purchase_date), but still exactly and on ONE column; an
// undeclared column or a window split across columns is still rejected.
func TestValidateWindowAcceptsADeclaredAlternateDateColumn(t *testing.T) {
	card := altDateCard{fakeCard: fakeCard{name: "procurement_loads_base", dateCol: "entered_business_day"}, alts: []string{"purchase_date"}}
	w := augustWindow()
	ok := "SELECT 'Loads' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.procurement_loads_base WHERE tenant_id = '" + tenant + "' AND purchase_date >= '2026-08-01' AND purchase_date < '2026-09-01' LIMIT 10"
	if err := ValidateWindow(ok, card, w); err != nil {
		t.Fatalf("declared alternate date column must bind the window: %v", err)
	}
	for name, sql := range map[string]string{
		"undeclared column":        "SELECT 'Loads' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.procurement_loads_base WHERE tenant_id = '" + tenant + "' AND created_on >= '2026-08-01' AND created_on < '2026-09-01' LIMIT 10",
		"split across two columns": "SELECT 'Loads' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.procurement_loads_base WHERE tenant_id = '" + tenant + "' AND purchase_date >= '2026-08-01' AND entered_business_day < '2026-09-01' LIMIT 10",
		"no window":                "SELECT 'Loads' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.procurement_loads_base WHERE tenant_id = '" + tenant + "' LIMIT 10",
	} {
		if err := ValidateWindow(sql, card, w); err == nil {
			t.Fatalf("%s: must still be rejected", name)
		}
	}
	// A card without the capability behaves exactly as before.
	if err := ValidateWindow(ok, card.fakeCard, w); err == nil {
		t.Fatal("a card declaring no alternates must only accept its business-day column")
	}
}
