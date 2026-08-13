package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

// Regression proof for an error-MAPPING defect found by an end-to-end run on 2026-08-06.
//
// A birth naming a pen that does not exist in the chosen shed was always refused correctly, and the
// transaction always rolled back correctly -- the DATA was never at risk. But the refusal surfaced as
// `500 internal_error`, so the operator was told the server had broken when the real answer was "that
// field is wrong". A 5xx also tells every client and every alerting rule that this is OUR fault and
// worth retrying; bad operator input is neither.
//
// The subtlety that caused it: identity's own mapRepoErr DOES map this sentinel, but the birth route
// never reaches it. Birth submits through the approvals path, so the mapping has to exist HERE. There
// are three separate error writers on this handler (writeAppError, writeCountsError,
// writeApprovalError) and the first fix went into the wrong one -- the retest still returned 500.
//
// This test asserts the STATUS and the CODE, deliberately. An assertion of merely "an error was
// returned" passes against the 500 and proves nothing.
func TestApprovalErrorMapsUnknownPartitionToBadRequest(t *testing.T) {
	handler := &AppWriteHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/counts/birth-events", nil)

	handler.writeApprovalError(rec, req, identityports.ErrPartitionNotInShed)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: an unknown pen is operator input, so it must never surface as a 5xx", rec.Code, http.StatusBadRequest)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (body=%q)", err, rec.Body.String())
	}
	if body.Code != "invalid_partition_label" {
		t.Fatalf("code = %q, want %q so the app can point at the field", body.Code, "invalid_partition_label")
	}
	if body.Message == "" || body.Message == "internal server error" {
		t.Fatalf("message = %q, want operator-readable copy naming the shed/partition problem", body.Message)
	}
}

// The sentinel must still be recognised when it arrives WRAPPED. Repository code wraps errors with
// %w as they travel up (`identity: create admin goat: ...: %w`), so a mapping that compares with ==
// instead of errors.Is would pass the bare-sentinel test above and still return 500 in production.
func TestApprovalErrorMapsWrappedUnknownPartitionToBadRequest(t *testing.T) {
	handler := &AppWriteHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/counts/birth-events", nil)

	wrapped := fmt.Errorf("identity: create admin goat: upsert goat_shed_partitions: %w", identityports.ErrPartitionNotInShed)
	handler.writeApprovalError(rec, req, wrapped)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for a WRAPPED unknown-partition error", rec.Code, http.StatusBadRequest)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Code != "invalid_partition_label" {
		t.Fatalf("code = %q, want %q", body.Code, "invalid_partition_label")
	}
}
