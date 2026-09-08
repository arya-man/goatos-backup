package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdempotencyKeyRequiresAppWriteLength(t *testing.T) {
	h := NewHandler(nil, nil)
	for _, tc := range []struct {
		name       string
		key        string
		wantOK     bool
		wantStatus int
		wantCode   string
	}{
		{name: "missing", wantStatus: http.StatusBadRequest, wantCode: "missing_idempotency_key"},
		{name: "short", key: "short", wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
		{name: "long", key: strings.Repeat("x", 201), wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
		{name: "valid", key: "leadership-task-1", wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/app/leadership-tasks", nil)
			if tc.key != "" {
				req.Header.Set("Idempotency-Key", tc.key)
			}
			rec := httptest.NewRecorder()

			got, ok := h.idempotencyKey(rec, req)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantOK {
				if got != tc.key {
					t.Fatalf("key = %q, want %q", got, tc.key)
				}
				return
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body %q missing %q", rec.Body.String(), tc.wantCode)
			}
		})
	}
}
