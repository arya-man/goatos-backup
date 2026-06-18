package authallow

import "testing"

func TestEmailSetAllowsOnlyVerifiedConfiguredEmails(t *testing.T) {
	verified := true
	unverified := false
	set, err := NewEmailSet([]string{" Ravi@Mesha.SG ", "abhishek@mesha.sg"})
	if err != nil {
		t.Fatalf("NewEmailSet: %v", err)
	}

	tests := []struct {
		name     string
		email    string
		verified *bool
		want     bool
	}{
		{name: "configured verified", email: "ravi@mesha.sg", verified: &verified, want: true},
		{name: "configured case insensitive", email: "RAVI@MESHA.SG", verified: &verified, want: true},
		{name: "configured unverified", email: "ravi@mesha.sg", verified: &unverified, want: false},
		{name: "configured missing verified claim", email: "ravi@mesha.sg", verified: nil, want: false},
		{name: "not configured", email: "hr@mesha.sg", verified: &verified, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.Allows(tt.email, tt.verified); got != tt.want {
				t.Fatalf("Allows=%v want %v", got, tt.want)
			}
		})
	}
}

func TestEmptyEmailSetAllowsExistingAuthBehavior(t *testing.T) {
	if !EmailSet(nil).Allows("", nil) {
		t.Fatal("empty allowlist should not change auth behavior")
	}
}

func TestEmailSetRejectsInvalidValues(t *testing.T) {
	for _, email := range []string{"", "missing-at", "@mesha.sg", "ravi@", "ravi@mesha.sg,hr@mesha.sg", "ravi @mesha.sg"} {
		t.Run(email, func(t *testing.T) {
			if _, err := NewEmailSet([]string{email}); err == nil {
				t.Fatal("invalid email accepted")
			}
		})
	}
}
