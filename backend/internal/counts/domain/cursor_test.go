package domain

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestProjectionExceptionCursorRoundTripAndValidation(t *testing.T) {
	in := ProjectionExceptionCursor{
		UpdatedAt:             time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		ProjectionExceptionID: "77000000-0000-4000-8000-000000000001",
	}
	encoded, err := EncodeProjectionExceptionCursor(in)
	if err != nil {
		t.Fatalf("EncodeProjectionExceptionCursor err=%v", err)
	}
	got, err := DecodeProjectionExceptionCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeProjectionExceptionCursor err=%v", err)
	}
	if !got.UpdatedAt.Equal(in.UpdatedAt) || got.ProjectionExceptionID != in.ProjectionExceptionID {
		t.Fatalf("cursor=%+v, want %+v", got, in)
	}

	badShape := base64.RawURLEncoding.EncodeToString([]byte(`{"k":"counts_projection_exception","u":"2026-06-30T10:00:00Z","i":"not-a-uuid"}`))
	badKind := base64.RawURLEncoding.EncodeToString([]byte(`{"k":"other","u":"2026-06-30T10:00:00Z","i":"77000000-0000-4000-8000-000000000001"}`))
	tooLarge := strings.Repeat("a", MaxProjectionExceptionCursorLength+1)
	for name, value := range map[string]string{"bad_shape": badShape, "bad_kind": badKind, "bad_base64": "not+url+base64", "too_large": tooLarge} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeProjectionExceptionCursor(value); err == nil {
				t.Fatal("DecodeProjectionExceptionCursor accepted malformed cursor")
			}
		})
	}
}
