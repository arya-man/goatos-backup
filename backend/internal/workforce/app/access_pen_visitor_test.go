package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// A pen visit is recorded from the Tasks module's "For me" tab, which only the Tasks Do tick
// (pen_visits.execute) opens (maintainer decision 2026-09-14). Ticking a park for someone
// whose access does not reach that tab -- a care operator with PC Care Do only, say -- would
// owe them visits they have no screen for, so the save refuses it against the SAME
// assignments it is about to store, and the refusal names what to tick.
func TestSavePersonAccessRefusesAPenVisitorWhoCannotReachTheTasksTab(t *testing.T) {
	tasksDo := domain.AccessModuleWrite{ModuleKey: "leadership_tasks", Mobile: []string{"view", "do"}}
	pcCareDo := domain.AccessModuleWrite{ModuleKey: "pc_care", Mobile: []string{"view", "do"}}
	cases := []struct {
		name    string
		modules []domain.AccessModuleWrite
		parks   []string
		wantErr bool
	}{
		{"Tasks Do + a park: allowed", []domain.AccessModuleWrite{tasksDo}, []string{"p1"}, false},
		{"PC Care Do only + a park: refused", []domain.AccessModuleWrite{pcCareDo}, []string{"p1"}, true},
		{"no modules + a park: refused", nil, []string{"p1"}, true},
		{"PC Care Do only, no park: allowed (nothing owed)", []domain.AccessModuleWrite{pcCareDo}, nil, false},
		{"Tasks View only + a park: refused (View does not open the tab)", []domain.AccessModuleWrite{{ModuleKey: "leadership_tasks", Mobile: []string{"view"}}}, []string{"p1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &captureAccessRepo{}
			svc := NewAccessService(repo)
			_, err := svc.SavePersonAccess(context.Background(), "t1", "actor", "person-1", domain.SavePersonAccessRequest{
				ScopeMode: "parks", ParkIDs: []string{"p1"}, Modules: tc.modules, PenVisitParkIDs: tc.parks,
			})
			if tc.wantErr {
				if !errors.Is(err, ErrPenVisitorCannotReachTasks) {
					t.Fatalf("err = %v, want ErrPenVisitorCannotReachTasks", err)
				}
				if repo.saved.PersonID != "" {
					t.Fatal("a refused save must write nothing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(repo.saved.PenVisitParkIDs) != len(tc.parks) {
				t.Fatalf("saved visitor parks %v, want %v", repo.saved.PenVisitParkIDs, tc.parks)
			}
		})
	}
}
