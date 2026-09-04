package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// The home park is where per-park modules put a person's work, so the editor must never
// let it be guessed: one ticked park is the home park, more than one needs an explicit
// choice, and the choice must be a ticked park.
func TestSavePersonAccessResolvesTheHomePark(t *testing.T) {
	cases := []struct {
		name     string
		req      domain.SavePersonAccessRequest
		wantHome string
		wantErr  string
	}{
		{name: "one park is the home park", req: domain.SavePersonAccessRequest{ScopeMode: "parks", ParkIDs: []string{"p1"}}, wantHome: "p1"},
		{name: "one park refuses a different home", req: domain.SavePersonAccessRequest{ScopeMode: "parks", ParkIDs: []string{"p1"}, HomeParkID: "p2"}, wantErr: "home park must be one of"},
		{name: "two parks need a choice", req: domain.SavePersonAccessRequest{ScopeMode: "parks", ParkIDs: []string{"p1", "p2"}}, wantErr: "choose the home park"},
		{name: "two parks accept a ticked home", req: domain.SavePersonAccessRequest{ScopeMode: "parks", ParkIDs: []string{"p1", "p2"}, HomeParkID: "p2"}, wantHome: "p2"},
		{name: "two parks refuse an unticked home", req: domain.SavePersonAccessRequest{ScopeMode: "parks", ParkIDs: []string{"p1", "p2"}, HomeParkID: "p3"}, wantErr: "home park must be one of"},
		{name: "tenant keeps an optional seat", req: domain.SavePersonAccessRequest{ScopeMode: "tenant", ParkIDs: []string{"p1"}, HomeParkID: "p1"}, wantHome: "p1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &captureAccessRepo{}
			svc := NewAccessService(repo)
			_, err := svc.SavePersonAccess(context.Background(), "t1", "actor", "person-1", tc.req)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if repo.saved.HomeParkID != tc.wantHome {
				t.Fatalf("saved home park = %q, want %q", repo.saved.HomeParkID, tc.wantHome)
			}
			if tc.req.ScopeMode == "tenant" && repo.saved.ParkIDs != nil {
				t.Fatalf("tenant mode saved park rows %v; want none", repo.saved.ParkIDs)
			}
		})
	}
}
