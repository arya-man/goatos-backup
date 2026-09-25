package app

import "testing"

// The feed store's question names both figures; they read like every other number on the phone.
func TestStockFiguresReadWithIndianGrouping(t *testing.T) {
	for in, want := range map[float64]string{
		10973.6: "10,973.6",
		99999:   "99,999",
		2000:    "2,000",
		12.5:    "12.5",
		250:     "250",
		1234567: "12,34,567",
		0:       "0",
		-1500:   "-1,500",
	} {
		if got := trimKg(in); got != want {
			t.Errorf("trimKg(%v) = %q, want %q", in, got, want)
		}
	}
}
