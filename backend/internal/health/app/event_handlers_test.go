package app

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"testing"
)

type deathRecorder struct{ held, resumed, closed int }

func (d *deathRecorder) HoldForDeathReview(context.Context, string, string) error {
	d.held++
	return nil
}
func (d *deathRecorder) ResumeAfterDeathRejected(context.Context, string, string) error {
	d.resumed++
	return nil
}
func (d *deathRecorder) CloseForApprovedDeath(context.Context, string, string) error {
	d.closed++
	return nil
}
func TestDeathLifecycleConsumesReportRejectAndApprovedDeath(t *testing.T) {
	store := &deathRecorder{}
	bus := eventbus.NewInProcessBus()
	NewDeathLifecycleHandler(store).Register(bus)
	base := eventbus.Event{TenantID: testTenant, Key: testGoat}
	for _, typ := range []string{EventCountsDeathReported, EventCountsDeathRejected} {
		base.Type = typ
		if err := bus.Publish(context.Background(), base); err != nil {
			t.Fatal(err)
		}
	}
	base.Type = EventGoatExited
	base.Payload = []byte(`{"goat_id":"30000000-0000-4000-8000-000000000001","exit_reason":"died"}`)
	if err := bus.Publish(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if store.held != 1 || store.resumed != 1 || store.closed != 1 {
		t.Fatalf("calls=%+v", store)
	}
}
func TestDeathLifecycleIgnoresSoldExit(t *testing.T) {
	store := &deathRecorder{}
	bus := eventbus.NewInProcessBus()
	NewDeathLifecycleHandler(store).Register(bus)
	if err := bus.Publish(context.Background(), eventbus.Event{Type: EventGoatExited, TenantID: testTenant, Key: testGoat, Payload: []byte(`{"exit_reason":"sold"}`)}); err != nil {
		t.Fatal(err)
	}
	if store.closed != 0 {
		t.Fatal("sold exit closed Health case")
	}
}
