package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func TestGoatShiftedHandlerIgnoresNonShedScopeWithoutPoisoningEvent(t *testing.T) {
	handler := NewGoatShiftedHandler(&fakeSweepRepo{})

	err := handler.HandleEvent(context.Background(), eventbus.Event{
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "10000000-0000-4000-8000-000000000001",
		Payload:  []byte(`{"scope_type":"tenant","scope_id":"00000000-0000-4000-8000-000000000001"}`),
	})
	if err != nil {
		t.Fatalf("HandleEvent returned poison error for non-shed scope: %v", err)
	}
}
