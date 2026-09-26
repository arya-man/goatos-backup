package app

import (
	"context"
	"regexp"
	"testing"
)

// TestNoPageCopyNamesAnInternalRoute (2026-09-26, Sales E2E): the Herd Register note told the farm
// "the table pages live goats from /goats/search under the top-bar scope" -- an API route in
// visible copy. Copy is read by people; routes are for code. Every served copy value is checked.
func TestNoPageCopyNamesAnInternalRoute(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	route := regexp.MustCompile(`(^|[\s(])/(goats|app|admin|api|admin-web|procurement|sales|feed-direction|verification)/[a-z_{}*-]+`)
	for _, page := range resp.Pages {
		for key, value := range page.Copy {
			if route.MatchString(value) {
				t.Errorf("page %s copy %q names an internal route: %q", page.RouteID, key, value)
			}
		}
	}
}
