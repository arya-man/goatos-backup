package kernelstages

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestSweeperConfigFromEnvRequiresActorID is the guard for the nil-TaskCreator
// footgun: without GOATOS_SWEEPER_ACTOR_ID the obligation sweeper's TaskCreator
// is nil and SOP-task creation for new batches silently no-ops. The config
// resolver must reject an empty actor id so a misconfigured deployment fails to
// boot rather than silently dropping SOP tasks forever.
func TestSweeperConfigFromEnvRequiresActorID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOATOS_SWEEPER_ACTOR_ID", "")

	_, err := SweeperConfigFromEnv()
	if err == nil {
		t.Fatal("expected SweeperConfigFromEnv to reject an empty GOATOS_SWEEPER_ACTOR_ID, got nil error")
	}
	if !strings.Contains(err.Error(), "GOATOS_SWEEPER_ACTOR_ID") {
		t.Fatalf("expected error to mention GOATOS_SWEEPER_ACTOR_ID, got: %v", err)
	}
}

// TestSweeperConfigFromEnvRequiresTenant confirms the tenant precondition is
// enforced too.
func TestSweeperConfigFromEnvRequiresTenant(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	t.Setenv("GOATOS_SWEEPER_ACTOR_ID", "actor-1")

	_, err := SweeperConfigFromEnv()
	if err == nil {
		t.Fatal("expected SweeperConfigFromEnv to reject an empty GOATOS_TENANT_ID, got nil error")
	}
	if !strings.Contains(err.Error(), "GOATOS_TENANT_ID") {
		t.Fatalf("expected error to mention GOATOS_TENANT_ID, got: %v", err)
	}
}

// TestSweeperConfigFromEnvSucceedsWithActor confirms a fully configured
// environment resolves cleanly and carries the actor id through.
func TestSweeperConfigFromEnvSucceedsWithActor(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOATOS_SWEEPER_ACTOR_ID", "actor-42")

	cfg, err := SweeperConfigFromEnv()
	if err != nil {
		t.Fatalf("expected clean config, got error: %v", err)
	}
	if cfg.ActorID != "actor-42" {
		t.Fatalf("expected ActorID=actor-42, got %q", cfg.ActorID)
	}
}

// TestBuildSweeperTaskCreatorNilWithoutActor asserts the exact silent-no-op
// condition the config guard protects against: an empty actor id yields a nil
// TaskCreator (SOP tasks would never be created), while a non-empty actor id
// yields a real creator.
func TestBuildSweeperTaskCreatorNilWithoutActor(t *testing.T) {
	if creator := buildSweeperTaskCreator(Deps{}, ""); creator != nil {
		t.Fatalf("expected nil TaskCreator for empty actor id, got %T", creator)
	}
	if creator := buildSweeperTaskCreator(Deps{}, "   "); creator != nil {
		t.Fatalf("expected nil TaskCreator for blank actor id, got %T", creator)
	}
	if creator := buildSweeperTaskCreator(Deps{}, "actor-1"); creator == nil {
		t.Fatal("expected a non-nil TaskCreator when actor id is set")
	}
}

// TestResolveDueBeforeDefaultsToBusinessDayBoundary is PEND-6 for the LIVE
// production sweeper path: ObligationSweeperStage.Run (the kernel-worker
// stage actually deployed) computed dueBefore := now, a raw wall-clock
// instant, while only the RETIRED cmd/obligation-sweeper one-shot got the
// business-day-boundary fix. A run late in the business day (say 23:50 IST)
// must still pick up obligations due later that same day, so two calls at
// different instants within the same business day must resolve to the
// IDENTICAL default dueBefore. An explicit cfg.DueBefore override must still
// win verbatim. This mirrors
// cmd/obligation-sweeper/main_test.go:TestParseFlagsDueBeforeDefaultsToBusinessDayBoundary.
func TestResolveDueBeforeDefaultsToBusinessDayBoundary(t *testing.T) {
	morning := time.Date(2026, 7, 19, 6, 0, 0, 0, biztime.DefaultLocation())
	evening := time.Date(2026, 7, 19, 23, 50, 0, 0, biztime.DefaultLocation())
	wantDueBefore := biztime.BusinessDayStart(morning).Add(24 * time.Hour)

	cfg := SweeperConfig{TenantID: "tenant-1", ActorID: "actor-1"}

	morningDueBefore := resolveDueBefore(cfg, morning)
	eveningDueBefore := resolveDueBefore(cfg, evening)

	if !morningDueBefore.Equal(wantDueBefore) {
		t.Fatalf("morning dueBefore = %s, want business-day end %s", morningDueBefore, wantDueBefore)
	}
	if !eveningDueBefore.Equal(wantDueBefore) {
		t.Fatalf("evening dueBefore = %s, want business-day end %s", eveningDueBefore, wantDueBefore)
	}
	if !morningDueBefore.Equal(eveningDueBefore) {
		t.Fatalf("dueBefore differs across same business day: morning=%s evening=%s", morningDueBefore, eveningDueBefore)
	}

	override := time.Date(2026, 7, 20, 10, 0, 0, 0, biztime.DefaultLocation())
	overrideCfg := SweeperConfig{TenantID: "tenant-1", ActorID: "actor-1", DueBefore: override}
	if got := resolveDueBefore(overrideCfg, evening); !got.Equal(override) {
		t.Fatalf("override dueBefore = %s, want verbatim override %s", got, override)
	}
}

// TestSweeperConfigFromEnvParsesDueBeforeOverride confirms the
// GOATOS_SWEEPER_DUE_BEFORE env override path resolves into cfg.DueBefore, so
// resolveDueBefore's override branch is reachable from real deployment config,
// not just from a hand-built SweeperConfig in tests.
func TestSweeperConfigFromEnvParsesDueBeforeOverride(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOATOS_SWEEPER_ACTOR_ID", "actor-1")
	t.Setenv("GOATOS_SWEEPER_DUE_BEFORE", "2026-07-20T10:00:00+05:30")

	cfg, err := SweeperConfigFromEnv()
	if err != nil {
		t.Fatalf("expected clean config, got error: %v", err)
	}
	want := time.Date(2026, 7, 20, 10, 0, 0, 0, biztime.DefaultLocation())
	if !cfg.DueBefore.Equal(want) {
		t.Fatalf("cfg.DueBefore = %s, want %s", cfg.DueBefore, want)
	}
}

// TestSweeperConfigFromEnvRejectsInvalidDueBefore ensures a malformed override
// fails fast at config-resolution time rather than silently falling back to
// the default cutoff.
func TestSweeperConfigFromEnvRejectsInvalidDueBefore(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("GOATOS_SWEEPER_ACTOR_ID", "actor-1")
	t.Setenv("GOATOS_SWEEPER_DUE_BEFORE", "not-a-timestamp")

	if _, err := SweeperConfigFromEnv(); err == nil {
		t.Fatal("expected SweeperConfigFromEnv to reject a non-RFC3339 GOATOS_SWEEPER_DUE_BEFORE, got nil error")
	}
}
