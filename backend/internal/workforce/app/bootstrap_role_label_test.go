package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// The phone's drawer header rendered the raw role code ("pc_director") under a director's name
// (Sales phone E2E 2026-09-26): bootstrap served only the code. It now serves the role in farm
// words beside it, in the caller's locale, and nothing at all for a code with no words.
func TestBootstrapServesTheRoleInFarmWords(t *testing.T) {
	cases := []struct{ hint, locale, want string }{
		{"pc_director", "en", "Director"},
		{"park_head", "en", "Park Head"},
		{"feed_director", "en", "Feed Director"},
		{"cxo", "en", "CXO"},
		{"pc_director", "hi", "निदेशक"},
		{"not_a_catalogued_role", "en", ""},
	}
	for _, tc := range cases {
		p := profile("active")
		p.PrimaryRoleHint = tc.hint
		svc := NewService(&fakeRepo{profile: p, grants: []domain.GrantSummary{grant()}})
		got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", tc.locale, "trace-1")
		if err != nil {
			t.Fatalf("Bootstrap(%s): %v", tc.hint, err)
		}
		if got.OperatorProfile.PrimaryRoleLabel != tc.want {
			t.Fatalf("hint %q locale %q: label %q, want %q", tc.hint, tc.locale, got.OperatorProfile.PrimaryRoleLabel, tc.want)
		}
		if got.OperatorProfile.PrimaryRoleHint != tc.hint {
			t.Fatalf("the code itself must still be served unchanged, got %q", got.OperatorProfile.PrimaryRoleHint)
		}
	}
}
