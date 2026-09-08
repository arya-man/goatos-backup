package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	notificationaudiencepg "github.com/vgoats/goatos/backend/internal/notificationaudience/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/app"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

const tenantID = "00000000-0000-4000-8000-00000000a0d1"

// positionsFake stands in for the workforce resolver so the round trip proves the STORED row is
// what the resolver reads, not what the fake returns.
type positionsFake struct{ calls []string }

func (f *positionsFake) ResolvePositionRecipients(_ context.Context, _, _, _, code string) ([]workforcedomain.NotificationRecipient, error) {
	f.calls = append(f.calls, code)
	return []workforcedomain.NotificationRecipient{{WorkforceMemberID: "m-" + code, DeviceID: "d-" + code, FCMToken: "t-" + code}}, nil
}

// TestAudienceRoundTripThroughTheStoredOverride is the production-path proof: an admin's save on
// the matrix changes who the resolver hands a notifier, a stale save is refused, an unknown
// designation is refused, and a reset restores the catalog default.
func TestAudienceRoundTripThroughTheStoredOverride(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'audience-test', 'active') ON CONFLICT DO NOTHING`, tenantID); err != nil {
		t.Fatal(err)
	}

	repo := notificationaudiencepg.NewRepository(pool, 5*time.Second)
	positions := &positionsFake{}
	resolver := app.NewResolver(positions).WithSubscriptions(repo)
	svc := app.NewConfigService(repo)

	// 1. Nothing stored: the catalog default is what the notifier gets.
	got, err := resolver.Recipients(ctx, tenantID, "", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions.calls, []string{"ceo_internal", "feed_director", "procurement_director"}) || len(got) != 3 {
		t.Fatalf("default resolution asked %v, got %d recipients", positions.calls, len(got))
	}
	matrix, err := svc.Matrix(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Designations) == 0 {
		t.Fatal("the designation catalog seeded by 000219 must be offered as columns")
	}
	for _, row := range matrix.Alerts {
		if row.Customised {
			t.Fatalf("fresh tenant must have no customised row, got %+v", row)
		}
	}

	// 2. An admin customises the alert to the Park Head only.
	saved, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{
		DesignationCodes: []string{"park_head"}, RowVersion: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Customised || saved.RowVersion != 1 || !reflect.DeepEqual(saved.Designations, []string{"park_head"}) {
		t.Fatalf("saved = %+v", saved)
	}
	positions.calls = nil
	got, err = resolver.Recipients(ctx, tenantID, "", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	// park_head is a PARK desk and this alert carries no park: nobody is resolved, and none of
	// the default desks is asked any more. The customised audience really replaced the default.
	if len(positions.calls) != 0 || len(got) != 0 {
		t.Fatalf("after override: asked %v, got %d", positions.calls, len(got))
	}
	got, err = resolver.Recipients(ctx, tenantID, "00000000-0000-4000-8000-0000000000aa", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions.calls, []string{"park_head"}) || len(got) != 1 || got[0].RoleLabel != "park_head" {
		t.Fatalf("with a park: asked %v, got %+v", positions.calls, got)
	}

	// 3. A stale save (the version the first screen loaded with) is refused, never merged.
	if _, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{
		DesignationCodes: []string{"ceo_internal"}, RowVersion: 0,
	}); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale first-save err = %v want ErrVersionConflict", err)
	}
	if _, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{
		DesignationCodes: []string{"ceo_internal"}, RowVersion: 7,
	}); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale update err = %v want ErrVersionConflict", err)
	}

	// 4. A designation the catalog does not carry is refused, never silently dropped.
	if _, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{
		DesignationCodes: []string{"ceo_internal", "chief_goat_officer"}, RowVersion: 1,
	}); !errors.Is(err, ports.ErrUnknownDesignation) {
		t.Fatalf("unknown designation err = %v", err)
	}

	// 5. An EMPTY audience is stored as a decision and resolves to nobody.
	empty, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{
		DesignationCodes: nil, RowVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Customised || empty.RowVersion != 2 || len(empty.Designations) != 0 {
		t.Fatalf("empty save = %+v", empty)
	}
	positions.calls = nil
	if got, err = resolver.Recipients(ctx, tenantID, "00000000-0000-4000-8000-0000000000aa", domain.AlertFeedLowStock); err != nil || len(got) != 0 || len(positions.calls) != 0 {
		t.Fatalf("empty audience resolved %+v via %v (err %v)", got, positions.calls, err)
	}

	// 6. Reset restores the catalog default for the notifier.
	reset, err := svc.Save(ctx, tenantID, "", domain.AlertFeedLowStock, app.SaveAudienceRequest{UseDefaults: true})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Customised || reset.RowVersion != 0 {
		t.Fatalf("reset = %+v", reset)
	}
	positions.calls = nil
	if _, err := resolver.Recipients(ctx, tenantID, "", domain.AlertFeedLowStock); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions.calls, []string{"ceo_internal", "feed_director", "procurement_director"}) {
		t.Fatalf("after reset asked %v", positions.calls)
	}

	// 7. Every write left an audit row.
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action IN ('notification_audience.replaced', 'notification_audience.reset')`, tenantID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatalf("audit rows = %d want 3 (two replaces, one reset)", audits)
	}
}
