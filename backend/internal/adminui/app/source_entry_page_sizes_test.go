package app

import (
	"context"
	"testing"
)

// TestSourceEntryLoadsPageSizesKeep200 pins the source-loads page sizes: the renderer defaults to
// the LARGEST option, and the New load supplier picker is built from the loads on that page, so the
// options must keep the board's historical 200 (TR-2 P1-6 follow-up: a 50 default dropped suppliers).
func TestSourceEntryLoadsPageSizesKeep200(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	page := pageByRouteID(t, resp.Pages, "source-entry")
	if len(page.Tables) != 1 || page.Tables[0].ID != "source-loads" {
		t.Fatalf("source-entry tables = %+v", page.Tables)
	}
	sizes := page.Tables[0].PageSizeOptions
	if len(sizes) == 0 || sizes[len(sizes)-1] != 200 {
		t.Fatalf("source-loads page sizes = %v, want the largest to be 200", sizes)
	}
}
