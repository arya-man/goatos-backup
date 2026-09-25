package app

import (
	"strings"
	"testing"
)

// The Sold page is READ-ONLY by contract: it declares no record control, so its empty ledger must
// not tell the reader to "record the first sale" -- an instruction the page cannot carry out. It
// names where sales ARE recorded instead. Sales Config, which does carry the record form, keeps
// its own prompt.
func TestSoldPageEmptyLedgerDoesNotAskTheReaderToRecord(t *testing.T) {
	sold := pageSpecificCopy("sales-sold")["empty.deals.unset"]
	if sold == "" {
		t.Fatal("sales-sold has no empty.deals.unset copy")
	}
	if strings.Contains(strings.ToLower(sold), "record the first sale") {
		t.Fatalf("read-only Sold page asks the reader to record a sale: %q", sold)
	}
	if !strings.Contains(sold, "Sales Config") {
		t.Fatalf("Sold page empty copy should name where sales are recorded: %q", sold)
	}
	config := pageSpecificCopy("sales-config")["empty.deals.unset"]
	if !strings.Contains(strings.ToLower(config), "record the first sale") {
		t.Fatalf("Sales Config lost its record prompt: %q", config)
	}
}
