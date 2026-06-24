package domain

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const validBatchRowID = "batch:70000000-0000-4000-8000-000000000008:rule:70000000-0000-4000-8000-000000000007:shed:70000000-0000-4000-8000-000000000002"

func TestDecodeCursorStrictlyValidatesShape(t *testing.T) {
	encoded, err := EncodeCursor(Cursor{
		SortPriority: 5,
		DueAt:        time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC),
		RowID:        validBatchRowID,
	})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if _, err := DecodeCursor(encoded); err != nil {
		t.Fatalf("DecodeCursor(valid) error = %v", err)
	}

	tests := map[string]string{
		"oversized":      strings.Repeat("a", MaxCursorLength+1),
		"bad_base64":     "not+url+base64",
		"unknown_field":  base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"` + validBatchRowID + `","x":1}`)),
		"bad_sort":       base64.RawURLEncoding.EncodeToString([]byte(`{"s":101,"d":"2026-06-24T09:00:00Z","r":"` + validBatchRowID + `"}`)),
		"bad_due_at":     base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"not-time","r":"` + validBatchRowID + `"}`)),
		"bad_row_id":     base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"batch:missing"}`)),
		"huge_payload":   base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"` + strings.Repeat("a", MaxRowIDLength+1) + `"}`)),
		"trailing_value": base64.RawURLEncoding.EncodeToString([]byte(`{"s":1,"d":"2026-06-24T09:00:00Z","r":"` + validBatchRowID + `"} {}`)),
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCursor(value); err == nil {
				t.Fatal("DecodeCursor() accepted malformed cursor")
			}
		})
	}
}

func TestValidateRowIDAllowsCurrentWorkflowKeys(t *testing.T) {
	for _, rowID := range []string{
		validBatchRowID,
		"obligation:71000000-0000-4000-8000-000000000013",
	} {
		if err := ValidateRowID(rowID); err != nil {
			t.Fatalf("ValidateRowID(%q) error = %v", rowID, err)
		}
	}
}

func TestValidateRowIDRejectsMalformedOrOversizedKeys(t *testing.T) {
	for _, rowID := range []string{
		"",
		"batch:missing",
		"batch:70000000-0000-4000-8000-000000000008:shed:70000000-0000-4000-8000-000000000002",
		"batch:" + strings.Repeat("a", MaxRowIDLength),
		"feed:70000000-0000-4000-8000-000000000008",
	} {
		if err := ValidateRowID(rowID); err == nil {
			t.Fatalf("ValidateRowID(%q) accepted malformed row id", rowID)
		}
	}
}
