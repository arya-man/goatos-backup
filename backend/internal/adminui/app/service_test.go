package app

import (
	"encoding/json"
	"strings"
	"testing"
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
