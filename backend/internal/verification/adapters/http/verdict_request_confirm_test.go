package http

import (
	"encoding/json"
	"testing"
)

// The verifier's confirmation rides the same measurement block as her readings (maintainer decision
// 2026-09-09). Pinned on the wire: absent is false, present is carried, and it never invents a
// measurement on its own.
func TestVerdictRequestCarriesVarianceAcknowledged(t *testing.T) {
	var body verdictRequest
	if err := json.Unmarshal([]byte(`{"decision":"approved","row_version":3,"measurement":{"entries":[{"key":"maize","value":12.6}],"variance_acknowledged":true}}`), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := body.toDomainMeasurement()
	if got == nil || !got.VarianceAcknowledged || len(got.Entries) != 1 {
		t.Fatalf("measurement = %+v, want the entry and VarianceAcknowledged", got)
	}

	var plain verdictRequest
	if err := json.Unmarshal([]byte(`{"decision":"approved","row_version":3,"measurement":{"entries":[{"key":"maize","value":12.6}]}}`), &plain); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if plain.toDomainMeasurement().VarianceAcknowledged {
		t.Fatal("an absent flag is the first press, never an acknowledgement")
	}

	var flagOnly verdictRequest
	if err := json.Unmarshal([]byte(`{"decision":"approved","row_version":3,"measurement":{"variance_acknowledged":true}}`), &flagOnly); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if flagOnly.toDomainMeasurement() != nil {
		t.Fatal("a flag with no reading is no measurement at all")
	}
}
