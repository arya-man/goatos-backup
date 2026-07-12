package domain

import (
	"encoding/base64"
	"testing"
)

func TestExecutionCursorRoundTrip(t *testing.T) {
	in := ExecutionCursor{SortRank: 3, SortDueMicros: 1_719_234_567_890_123, SortRowKey: "park|shed|rule|batch"}
	encoded, err := EncodeExecutionCursor(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeExecutionCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("cursor = %#v, want %#v", got, in)
	}
}

func TestExecutionCursorRejectsMalformedPayloads(t *testing.T) {
	tests := []string{
		"not-base64",
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"r":1,"d":1,"k":"x"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"r":99,"d":1,"k":"x"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"r":1,"d":1,"k":"x","extra":true}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"r":1,"d":1,"k":"x"} {}`)),
	}
	for _, value := range tests {
		if _, err := DecodeExecutionCursor(value); err == nil {
			t.Fatalf("DecodeExecutionCursor(%q) unexpectedly succeeded", value)
		}
	}
}
