package postgres

import (
	"context"
	"errors"
	"testing"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
)

// TestCaptureCardVersionMalformedPinIsUnknown: a sop_version_id that is not a UUID can name no
// version -- 409 capture_sop_version_unknown (refresh the card), never a 500 from Postgres'
// uuid cast. Answered before any query. Found live by the herd-ops E2E.
func TestCaptureCardVersionMalformedPinIsUnknown(t *testing.T) {
	source := NewCaptureCardSource(nil, 0)
	if _, err := source.CaptureCardVersion(context.Background(), "00000000-0000-4000-8000-000000000001", "counts.birth", "garbage"); !errors.Is(err, countsapp.ErrCaptureSOPVersionUnknown) {
		t.Fatalf("err = %v, want ErrCaptureSOPVersionUnknown", err)
	}
}
