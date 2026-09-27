package app

import (
	"context"
	"strings"
	"testing"
)

// O41: the Action Center empty state rendered action.open_config and action.open_sops side by side with
// the same copy and target ("Open the vaccination plan" twice). Protocol rules and the vaccination SOP
// both live on the vaccination plan, so the page carries ONE CTA key, and no two action.open_* keys on
// these pages may read the same.
func TestVaccinationBoardOpenCTAsAreDistinct(t *testing.T) {
	pages := NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages
	for _, id := range []string{"action-center", "protocol-adherence"} {
		page := pageByRouteID(t, pages, id)
		if _, ok := page.Copy["action.open_sops"]; ok {
			t.Fatalf("%s still publishes action.open_sops (a second vaccination-plan CTA)", id)
		}
		seen := map[string]string{}
		for key, value := range page.Copy {
			if !strings.HasPrefix(key, "action.open_") {
				continue
			}
			if other, dup := seen[value]; dup {
				t.Fatalf("%s: %s and %s both read %q", id, other, key, value)
			}
			seen[value] = key
		}
	}
	if got := pageByRouteID(t, pages, "action-center").Copy["action.open_config"]; got != "Open the vaccination plan" {
		t.Fatalf("action-center empty-state CTA = %q", got)
	}
}
