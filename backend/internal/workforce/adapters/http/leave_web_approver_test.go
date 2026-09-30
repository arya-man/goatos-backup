package workforcehttp

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/app"
)

// On the web every leave decision is the HR line (maintainer, 2026-09-30): the CEO acts as HR, HR
// is HR, and a park head -- who decides on the phone -- decides nothing on the web.
func TestWebLeaveDecisionsAreHRLevelOnly(t *testing.T) {
	cases := []struct {
		name string
		in   app.LeaveApprover
		hr   bool
	}{
		{"ceo", app.LeaveApprover{Any: true}, true},
		{"hr", app.LeaveApprover{HR: true}, true},
		{"park head", app.LeaveApprover{ParkHeadParks: []string{"p"}, ParkHeadOfHomePark: true}, false},
		{"hr and park head", app.LeaveApprover{HR: true, ParkHeadParks: []string{"p"}}, true},
	}
	for _, c := range cases {
		got := webLeaveApprover(c.in)
		if got.Any || len(got.ParkHeadParks) > 0 || got.ParkHeadOfHomePark || got.HR != c.hr {
			t.Errorf("%s: web approver = %+v, want only HR=%v", c.name, got, c.hr)
		}
	}
}
