package postgres

import "testing"

// The auto-created auth profile is named after the PERSON (email local-part),
// never after the role: the literal "CEO/CXO" placeholder produced N identical
// unidentifiable rows in the People/HRMS directory (2026-08-22).
func TestPendingEmailGrantDisplayNameIsThePersonNotTheRole(t *testing.T) {
	cases := map[string]string{
		"ravi@mesha.sg":      "Ravi",
		"manohar.k@mesha.sg": "Manohar K",
		"jyothi_pvg@x.com":   "Jyothi Pvg",
	}
	for email, want := range cases {
		if got := pendingEmailGrantDisplayName(email, "ceo_internal"); got != want {
			t.Fatalf("pendingEmailGrantDisplayName(%q) = %q, want %q", email, got, want)
		}
	}
	if got := pendingEmailGrantDisplayName("", "ceo_internal"); got != "CEO/CXO" {
		t.Fatalf("empty email fallback = %q, want CEO/CXO", got)
	}
	if got := pendingEmailGrantDisplayName("", "operator"); got != "Granted user" {
		t.Fatalf("empty email non-leadership fallback = %q", got)
	}
}
