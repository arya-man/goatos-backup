package kernelstages

import (
	"strings"
	"testing"
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
