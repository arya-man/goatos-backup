package main

import (
	"strings"
	"testing"
)

const tenant = "-tenant=00000000-0000-4000-8000-000000000001"

// The command migration 000190 tells an operator to type must be a command this binary
// accepts. A deploy instruction the binary rejects is worse than no instruction: it is
// followed, it fails, and the step gets improvised at exactly the wrong moment.
func TestTheCommandTheMigrationDocumentsIsAccepted(t *testing.T) {
	cfg, _, err := parseFlags([]string{tenant, "-mode", "drafts", "-apply"})
	if err != nil {
		t.Fatalf("the documented deploy command was rejected: %v", err)
	}
	if !cfg.Apply {
		t.Fatal("-apply parsed but did not apply")
	}
	if cfg.Mode != "drafts" {
		t.Fatalf("mode = %q, want drafts", cfg.Mode)
	}
}

// Reporting is the default. A repair that mutates unasked is one mistyped flag from
// retiring live work.
func TestApplyingIsOptIn(t *testing.T) {
	for _, args := range [][]string{
		{tenant},
		{tenant, "-mode", "repeat"},
		{tenant, "-dry-run"},
		{tenant, "-apply=false"},
	} {
		cfg, _, err := parseFlags(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if cfg.Apply {
			t.Fatalf("%v applied changes without being asked to", args)
		}
	}
}

// Both spellings mean the same thing, because both are in use.
func TestEitherSpellingApplies(t *testing.T) {
	for _, args := range [][]string{
		{tenant, "-apply"},
		{tenant, "-dry-run=false"},
		{tenant, "-apply=true", "-dry-run=false"},
	} {
		cfg, _, err := parseFlags(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !cfg.Apply {
			t.Fatalf("%v did not apply", args)
		}
	}
}

// Contradicting yourself is refused, not resolved by precedence: whichever way it were
// guessed, half the time it would do the opposite of what was asked.
func TestContradictoryFlagsAreRefused(t *testing.T) {
	for _, args := range [][]string{
		{tenant, "-apply", "-dry-run"},
		{tenant, "-apply=false", "-dry-run=false"},
	} {
		if _, _, err := parseFlags(args); err == nil {
			t.Fatalf("%v was accepted; it says two different things", args)
		} else if !strings.Contains(err.Error(), "contradict") {
			t.Fatalf("%v error = %v, want it to name the contradiction", args, err)
		}
	}
}

func TestTenantIsRequiredAndModeIsChecked(t *testing.T) {
	if _, _, err := parseFlags([]string{"-mode", "drafts"}); err == nil {
		t.Fatal("a tenant-less run was accepted")
	}
	if _, _, err := parseFlags([]string{tenant, "-mode", "nonsense"}); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
}
