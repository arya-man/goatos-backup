package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
)

func TestBootstrapPublishesAdminWebContract(t *testing.T) {
	resp := NewService().Bootstrap()
	if resp.SchemaVersion != "admin-web-ui-v1" {
		t.Fatalf("schema version = %q", resp.SchemaVersion)
	}
	if len(resp.Navigation.Primary) == 0 || len(resp.Navigation.Groups) == 0 {
		t.Fatalf("navigation contract is empty: %#v", resp.Navigation)
	}
	if len(resp.RouteLabels) == 0 || len(resp.Pages) == 0 {
		t.Fatalf("route/page contracts missing: labels=%d pages=%d", len(resp.RouteLabels), len(resp.Pages))
	}
	if resp.Navigation.Primary[0].Label != "Control Tower" {
		t.Fatalf("first primary nav = %#v", resp.Navigation.Primary[0])
	}
}

func TestBootstrapJSONDoesNotPublishNullCollections(t *testing.T) {
	raw, err := json.Marshal(NewService().Bootstrap())
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	if strings.Contains(string(raw), ":null") {
		t.Fatalf("bootstrap contract contains null collection fields: %s", raw)
	}
}

func TestBootstrapDoesNotPublishHardcodedLocationTruth(t *testing.T) {
	raw, err := json.Marshal(NewService().Bootstrap())
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	for _, forbidden := range []string{
		"00000000-0000-4000-8000-000000003001",
		"00000000-0000-4000-8000-000000003002",
		"park:CBE",
		"park:CPT",
		"Coimbatore",
		"Channapatna",
		"R. Teja",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("bootstrap contract contains hardcoded live-location/person truth %q", forbidden)
		}
	}
}

func TestActionCenterParkDisplayChipsAreOptionalDbCompiledOverrides(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap().Pages, "action-center")
	group := optionGroupByID(t, page.OptionGroups, "park_display_chips")

	if len(group.Options) != 0 {
		t.Fatalf("phase-0 bootstrap must not publish static park chip options; locations must be DB-compiled, got %#v", group.Options)
	}
}

func pageByRouteID(t *testing.T, pages []domain.PageContract, routeID string) domain.PageContract {
	t.Helper()
	for _, page := range pages {
		if page.RouteID == routeID {
			return page
		}
	}
	t.Fatalf("missing page contract %q", routeID)
	return domain.PageContract{}
}

func optionGroupByID(t *testing.T, groups []domain.OptionGroup, id string) domain.OptionGroup {
	t.Helper()
	for _, group := range groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("missing option group %q", id)
	return domain.OptionGroup{}
}
