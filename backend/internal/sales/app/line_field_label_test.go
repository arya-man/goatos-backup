package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// A line refusal names the line in farm words, never the raw key "lines[1].rate_per_unit".
func TestLineRefusalNeverStartsWithARawKey(t *testing.T) {
	e := SalesHTTPError(domain.ErrDealValidation{Field: "lines[2].rate_per_unit", Reason: "required"})
	if !strings.HasPrefix(e.Message, "Line 2 rate required") || strings.Contains(e.Message, "lines[") {
		t.Fatalf("message = %q", e.Message)
	}
	if e.Code != "sales_invalid_lines[2].rate_per_unit" {
		t.Fatalf("the code keeps the field for clients that key on it: %q", e.Code)
	}
}
