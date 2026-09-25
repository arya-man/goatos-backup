package postgres

import (
	"strings"
	"testing"
)

// dealLineScanTargets is the number of destinations attachDealLineRows passes to rows.Scan.
const dealLineScanTargets = 18

// selectItemCount counts the top-level select-list items between SELECT and FROM.
func selectItemCount(t *testing.T, query string) int {
	t.Helper()
	upper := strings.ToUpper(query)
	start := strings.Index(upper, "SELECT")
	end := strings.Index(upper, "FROM PUBLIC.SALES_DEAL_LINES")
	if start < 0 || end < 0 {
		t.Fatalf("cannot find select list in %q", query)
	}
	depth, items := 0, 1
	for _, r := range query[start+len("SELECT") : end] {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items++
			}
		}
	}
	return items
}

// Regression (STG 396f21c96): 000422 grew the deal-line scanner to 18 columns while the overview's
// closed-deal line query still selected 13, so every GET /sales/overview failed with "number of
// field descriptions must equal number of destinations, got 13 and 18" -> 500 internal_error.
func TestEveryDealLineReadMatchesTheSharedScanner(t *testing.T) {
	for name, q := range map[string]string{
		"dealLinesForPageSQL":        dealLinesForPageSQL,
		"dealLinesForClosedDealsSQL": dealLinesForClosedDealsSQL,
	} {
		if got := selectItemCount(t, q); got != dealLineScanTargets {
			t.Errorf("%s selects %d columns, attachDealLineRows scans %d", name, got, dealLineScanTargets)
		}
	}
}
