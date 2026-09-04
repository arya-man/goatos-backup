package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Ticking every screen of a module stores the EMPTY list ("every page"), so a screen added
// later reaches the person without a re-save; unticking one screen stores the explicit
// remainder. This is what stops the 2026-09-03 ADG Analytics loss from recurring.
func TestEveryScreenTickedIsStoredAsEveryPage(t *testing.T) {
	all := permissions.PageKeysForModule("weighing")
	if len(all) < 2 {
		t.Skipf("weighing has %d web screens; need at least two for this test", len(all))
	}
	repo := &captureAccessRepo{}
	svc := NewAccessService(repo)
	save := func(pages []string) []string {
		t.Helper()
		if _, err := svc.SavePersonAccess(context.Background(), "t1", "actor", "person-1", domain.SavePersonAccessRequest{
			ScopeMode: "tenant",
			Modules: []domain.AccessModuleWrite{
				{ModuleKey: "weighing", Web: []string{permissions.LevelView, permissions.LevelConfigure}, Pages: pages},
				{ModuleKey: "config", Web: []string{permissions.LevelView}},
			},
		}); err != nil {
			t.Fatalf("save %v: %v", pages, err)
		}
		for _, a := range repo.saved.Assignments {
			if a.Module == "weighing" && a.Surface == permissions.SurfaceWeb {
				return a.Pages
			}
		}
		t.Fatal("weighing web row not saved")
		return nil
	}
	if got := save(all); len(got) != 0 {
		t.Fatalf("every screen ticked stored %v; want the empty list meaning every page", got)
	}
	if got := save(all[:len(all)-1]); len(got) != len(all)-1 {
		t.Fatalf("one screen unticked stored %v; want the explicit %d remaining", got, len(all)-1)
	}
	// A retired key sent by an older client counts as its replacement.
	stale := append([]string{"weighing-weights"}, all[1:]...)
	if all[0] == "weighing-analytics" {
		if got := save(stale); len(got) != 0 {
			t.Fatalf("retired key plus the rest stored %v; want every page", got)
		}
	}
}
