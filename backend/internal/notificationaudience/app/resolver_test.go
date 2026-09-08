package app

import (
	"context"
	"errors"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type positionCall struct{ scopeType, scopeID, code string }

// positionsFake mirrors the real resolver's shape: a device per (scope, code) seat.
type positionsFake struct {
	calls []positionCall
	seats map[string][]workforcedomain.NotificationRecipient // key: scopeType|scopeID|code
}

func (f *positionsFake) ResolvePositionRecipients(_ context.Context, _, scopeType, scopeID, code string) ([]workforcedomain.NotificationRecipient, error) {
	f.calls = append(f.calls, positionCall{scopeType, scopeID, code})
	return f.seats[scopeType+"|"+scopeID+"|"+code], nil
}

type repoFake struct {
	stored map[string]ports.Audience
	err    error
}

func (r *repoFake) ListAudiences(context.Context, string) (map[string]ports.Audience, error) {
	return r.stored, r.err
}
func (r *repoFake) LoadAudience(_ context.Context, _, key string) (ports.Audience, bool, error) {
	if r.err != nil {
		return ports.Audience{}, false, r.err
	}
	a, ok := r.stored[key]
	return a, ok, nil
}
func (r *repoFake) ReplaceAudience(context.Context, ports.ReplaceAudienceCommand) (ports.Audience, error) {
	return ports.Audience{}, nil
}
func (r *repoFake) ResetAudience(context.Context, string, string, string) error { return nil }
func (r *repoFake) ListDesignations(context.Context) ([]ports.Designation, error) {
	return nil, nil
}

const tenant = "00000000-0000-4000-8000-000000000001"
const park = "00000000-0000-4000-8000-0000000000aa"

func device(member, id string) workforcedomain.NotificationRecipient {
	return workforcedomain.NotificationRecipient{WorkforceMemberID: member, DeviceID: id, FCMToken: "fcm-" + id}
}

func newPositions() *positionsFake {
	return &positionsFake{seats: map[string][]workforcedomain.NotificationRecipient{
		"tenant|" + tenant + "|ceo_internal":         {device("m-ceo", "d-ceo")},
		"tenant|" + tenant + "|feed_director":        {device("m-feed", "d-feed")},
		"tenant|" + tenant + "|procurement_director": {device("m-proc", "d-proc")},
		"tenant|" + tenant + "|pc_director":          {device("m-pc", "d-pc")},
		"center|" + park + "|park_head":              {device("m-ph", "d-ph")},
		"tenant|" + tenant + "|verifier":             {device("m-ver", "d-ver")},
	}}
}

func TestResolverServesCatalogDefaultsWithoutAStore(t *testing.T) {
	positions := newPositions()
	got, err := NewResolver(positions).Recipients(context.Background(), tenant, "", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].DeviceID != "d-ceo" || got[1].DeviceID != "d-feed" || got[2].DeviceID != "d-proc" {
		t.Fatalf("default low-stock audience = %+v", got)
	}
	// The CEO desk keeps the "ceo" observability label every queue row has always carried.
	if got[0].RoleLabel != "ceo" || got[1].RoleLabel != "feed_director" {
		t.Fatalf("role labels = %q %q", got[0].RoleLabel, got[1].RoleLabel)
	}
	if len(positions.calls) != 3 || positions.calls[0].scopeType != "tenant" || positions.calls[0].scopeID != tenant {
		t.Fatalf("calls = %+v", positions.calls)
	}
}

func TestResolverUsesTheStoredOverrideWhenOneExists(t *testing.T) {
	positions := newPositions()
	repo := &repoFake{stored: map[string]ports.Audience{
		domain.AlertFeedLowStock: {AlertKey: domain.AlertFeedLowStock, DesignationCodes: []string{"pc_director"}, RowVersion: 1},
	}}
	got, err := NewResolver(positions).WithSubscriptions(repo).Recipients(context.Background(), tenant, "", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DeviceID != "d-pc" || got[0].RoleLabel != "pc_director" {
		t.Fatalf("override audience = %+v", got)
	}
	for _, call := range positions.calls {
		if call.code == "ceo_internal" || call.code == "feed_director" {
			t.Fatalf("default desk %s was still resolved after the override: %+v", call.code, positions.calls)
		}
	}
}

// An EMPTY override is a decision ("nobody"), not a missing one: it must not fall back to the
// default, and it must resolve no desk at all.
func TestResolverHonoursAnEmptyOverrideAsNobody(t *testing.T) {
	positions := newPositions()
	repo := &repoFake{stored: map[string]ports.Audience{
		domain.AlertProcurementLoadOverdue: {AlertKey: domain.AlertProcurementLoadOverdue, DesignationCodes: []string{}, RowVersion: 2},
	}}
	got, err := NewResolver(positions).WithSubscriptions(repo).Recipients(context.Background(), tenant, "", domain.AlertProcurementLoadOverdue)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(positions.calls) != 0 {
		t.Fatalf("empty override resolved %+v via %+v", got, positions.calls)
	}
}

func TestResolverScopesParkDesksToTheParkAndSkipsThemWithoutOne(t *testing.T) {
	positions := newPositions()
	r := NewResolver(positions)
	withPark, err := r.Recipients(context.Background(), tenant, park, domain.AlertVaccinationWorkMissed)
	if err != nil {
		t.Fatal(err)
	}
	if len(withPark) != 2 || withPark[0].DeviceID != "d-ph" || withPark[0].RoleLabel != "park_head" || withPark[1].DeviceID != "d-pc" {
		t.Fatalf("with park = %+v", withPark)
	}
	if positions.calls[0].scopeType != "center" || positions.calls[0].scopeID != park {
		t.Fatalf("park head must resolve at center scope of the park, got %+v", positions.calls[0])
	}

	positions.calls = nil
	withoutPark, err := r.Recipients(context.Background(), tenant, "", domain.AlertVaccinationWorkMissed)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutPark) != 1 || withoutPark[0].DeviceID != "d-pc" {
		t.Fatalf("without park = %+v", withoutPark)
	}
	for _, call := range positions.calls {
		if call.code == "park_head" {
			t.Fatalf("a park desk must not be resolved without a park (it would guess one): %+v", positions.calls)
		}
	}
}

func TestResolverDedupesOneDeviceHoldingTwoDesks(t *testing.T) {
	positions := newPositions()
	shared := device("m-both", "d-both")
	positions.seats["tenant|"+tenant+"|feed_director"] = []workforcedomain.NotificationRecipient{shared}
	positions.seats["tenant|"+tenant+"|procurement_director"] = []workforcedomain.NotificationRecipient{shared}
	got, err := NewResolver(positions).Recipients(context.Background(), tenant, "", domain.AlertFeedLowStock)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("one device on two desks must be pushed once: %+v", got)
	}
}

// An ADDRESSED alert keeps the addressed person only while their title is ticked, and copies
// every other ticked title.
func TestResolverAddressedGatesOnTheAddresseeTitleAndCopiesOthers(t *testing.T) {
	positions := newPositions()
	cxo := []calendarports.NotificationRecipient{{MemberID: "m-cxo", DeviceID: "d-cxo", FCMToken: "t", RoleLabel: "ceo"}}
	key := domain.AlertLeadershipTaskRaised

	got, err := NewResolver(positions).Addressed(context.Background(), tenant, "", key, []string{domain.DesignationCEO}, cxo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DeviceID != "d-cxo" || len(positions.calls) != 0 {
		t.Fatalf("default addressed = %+v calls %+v", got, positions.calls)
	}

	repo := &repoFake{stored: map[string]ports.Audience{key: {AlertKey: key, DesignationCodes: []string{"pc_director"}, RowVersion: 1}}}
	got, err = NewResolver(positions).WithSubscriptions(repo).Addressed(context.Background(), tenant, "", key, []string{domain.DesignationCEO}, cxo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DeviceID != "d-pc" {
		t.Fatalf("unticked addressee = %+v", got)
	}

	repo.stored[key] = ports.Audience{AlertKey: key, DesignationCodes: []string{"ceo_internal", "pc_director"}, RowVersion: 2}
	got, err = NewResolver(positions).WithSubscriptions(repo).Addressed(context.Background(), tenant, "", key, []string{domain.DesignationCEO}, cxo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].DeviceID != "d-cxo" || got[1].DeviceID != "d-pc" {
		t.Fatalf("both ticked = %+v", got)
	}

	repo.stored[key] = ports.Audience{AlertKey: key, DesignationCodes: []string{}, RowVersion: 3}
	if got, err = NewResolver(positions).WithSubscriptions(repo).Addressed(context.Background(), tenant, "", key, []string{domain.DesignationCEO}, cxo); err != nil || len(got) != 0 {
		t.Fatalf("empty = %+v err %v", got, err)
	}
}

func TestResolverRefusesAnUnknownAlertLoudly(t *testing.T) {
	_, err := NewResolver(newPositions()).Recipients(context.Background(), tenant, "", "weighing.no_such_alert")
	if !errors.Is(err, ports.ErrUnknownAlert) {
		t.Fatalf("err = %v want ErrUnknownAlert", err)
	}
}

func TestResolverSurfacesAStoreReadFailureRatherThanGuessing(t *testing.T) {
	boom := errors.New("db down")
	_, err := NewResolver(newPositions()).WithSubscriptions(&repoFake{err: boom}).Recipients(context.Background(), tenant, "", domain.AlertFeedLowStock)
	if !errors.Is(err, boom) {
		t.Fatalf("a store failure must surface (retryable), got %v", err)
	}
}
