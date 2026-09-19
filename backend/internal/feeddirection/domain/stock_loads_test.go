package domain

import "testing"

// TestStockLoadGapIsAbsentWheneverEitherSideIsUnknown pins the check's absence rule: no stated
// figure, or no days-left projection, means NO check -- never a zero that reads as a pass.
func TestStockLoadGapIsAbsentWheneverEitherSideIsUnknown(t *testing.T) {
	said, left := int64(10), int64(3)
	if got := StockLoadGap(nil, 4, &left); got != nil {
		t.Fatalf("no figure stated -> nil, got %d", *got)
	}
	if got := StockLoadGap(&said, 4, nil); got != nil {
		t.Fatalf("no days-left projection -> nil, got %d", *got)
	}
	if got := StockLoadGap(&said, 4, &left); got == nil || *got != 3 {
		t.Fatalf("10 said - 4 consumed - 3 left = 3, got %v", got)
	}
	short := int64(2)
	if got := StockLoadGap(&short, 4, &left); got == nil || *got != -5 {
		t.Fatalf("2 said - 4 consumed - 3 left = -5 (the highlighted shortfall), got %v", got)
	}
}

func TestNormaliseStockLoadsPageRejectsPresentOutOfRangeAndDefaultsAbsent(t *testing.T) {
	if limit, offset, err := NormaliseStockLoadsPage(0, 0); err != nil || limit != DefaultStockLoadsPageSize || offset != 0 {
		t.Fatalf("absent limit takes the default: (%d, %d, %v)", limit, offset, err)
	}
	for _, tc := range [][2]int{{-1, 0}, {MaxStockLoadsPageSize + 1, 0}, {10, -1}, {10, MaxStockLoadsOffset + 1}} {
		if _, _, err := NormaliseStockLoadsPage(tc[0], tc[1]); err == nil {
			t.Fatalf("(%d, %d) must be refused", tc[0], tc[1])
		}
	}
}
