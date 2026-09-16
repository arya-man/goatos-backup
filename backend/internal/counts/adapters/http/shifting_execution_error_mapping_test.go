package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

func TestShiftingExecutionErrorMapsIdentityWriteConflictToConflict(t *testing.T) {
	handler := &AppWriteHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/counts/shifting-events/event-1/complete", nil)

	wrapped := fmt.Errorf("identity: relocate goats: verify expected source placement: %w", identityports.ErrWriteConflict)
	handler.writeShiftingExecutionError(rec, req, wrapped)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d for stale source-placement conflict", rec.Code, http.StatusConflict)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (body=%q)", err, rec.Body.String())
	}
	if body.Code != "shifting_source_changed" {
		t.Fatalf("code = %q, want shifting_source_changed", body.Code)
	}
	if body.Message == "" || body.Message == "internal server error" {
		t.Fatalf("message = %q, want an operator-readable stale movement message", body.Message)
	}
}

// SHIFTING SOP (2026-09-16): the judge's refusals reach the phone by name.
func TestShiftingExecutionErrorMapsSOPRefusalsByName(t *testing.T) {
	handler := &AppWriteHandler{}
	cases := []struct {
		err         error
		status      int
		code        string
		slot, quest string
	}{
		{fmt.Errorf("%w: %w", ports.ErrShiftingProofSlotInvalid, &authored.ProofError{SlotKey: "feed_clip", Message: "Record: Feed clip"}), http.StatusUnprocessableEntity, "shifting_proof_slot_invalid", "feed_clip", ""},
		{fmt.Errorf("%w: %w", ports.ErrShiftingAnswerInvalid, &authored.AnswerError{QuestionID: "calm", Message: "Answer: Animals calm?"}), http.StatusUnprocessableEntity, "shifting_answer_invalid", "", "calm"},
		{ports.ErrShiftingSOPVersionUnknown, http.StatusConflict, "shifting_sop_version_unknown", "", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/app/counts/shifting-events/event-1/complete", nil)
		handler.writeShiftingExecutionError(rec, req, c.err)
		if rec.Code != c.status {
			t.Fatalf("%v: status=%d want %d", c.err, rec.Code, c.status)
		}
		var body struct {
			Code     string `json:"code"`
			Slot     string `json:"slot"`
			Question string `json:"question"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != c.code || body.Slot != c.slot || body.Question != c.quest {
			t.Fatalf("%v: body=%+v", c.err, body)
		}
	}
}
