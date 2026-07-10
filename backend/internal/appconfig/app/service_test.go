package app

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

type fakeOwnership struct {
	owned []permissions.OwnedModule
	err   error
}

func (f fakeOwnership) ListActiveModuleGrantsForActor(_ context.Context, _, _ string) ([]permissions.OwnedModule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.owned, nil
}

func TestCompileIsDeterministicForSameInput(t *testing.T) {
	owned := []permissions.OwnedModule{{Vertical: "preventive_care", Module: "vaccination"}}
	svc := NewService(fakeOwnership{owned: owned}, ConfigFromEnv())

	first, err := svc.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	second, err := svc.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if first.Revision == "" {
		t.Fatal("revision must not be empty")
	}
	if first.Revision != second.Revision {
		t.Fatalf("revision not stable across identical compiles: %q vs %q", first.Revision, second.Revision)
	}
	if first.CachePolicy.ETag != `W/"`+first.Revision+`"` {
		t.Fatalf("etag = %q want weak-tagged revision", first.CachePolicy.ETag)
	}
}

func TestCompileRevisionChangesWithOwnedModules(t *testing.T) {
	svcEmpty := NewService(fakeOwnership{owned: nil}, ConfigFromEnv())
	empty, err := svcEmpty.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	svcOwned := NewService(fakeOwnership{owned: []permissions.OwnedModule{{Vertical: "preventive_care", Module: "vaccination"}}}, ConfigFromEnv())
	owned, err := svcOwned.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if empty.Revision == owned.Revision {
		t.Fatalf("revision must change when owned modules differ: %q", empty.Revision)
	}
	if len(owned.OwnedModules) != 1 || owned.OwnedModules[0].Module != "vaccination" {
		t.Fatalf("owned modules = %#v", owned.OwnedModules)
	}
	if len(empty.OwnedModules) != 0 {
		t.Fatalf("empty owned modules = %#v want empty slice not nil", empty.OwnedModules)
	}
}

func TestCompileFailsOpenOnMissingActorContext(t *testing.T) {
	svc := NewService(fakeOwnership{owned: []permissions.OwnedModule{{Vertical: "x", Module: "y"}}}, ConfigFromEnv())
	resp, err := svc.Compile(context.Background(), Input{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(resp.OwnedModules) != 0 {
		t.Fatalf("owned modules with no tenant/actor = %#v want empty (ownership read skipped)", resp.OwnedModules)
	}
}

func TestCompilePropagatesOwnershipReadError(t *testing.T) {
	svc := NewService(fakeOwnership{err: assertError("boom")}, ConfigFromEnv())
	_, err := svc.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err == nil {
		t.Fatal("Compile() error = nil want propagated ownership read error")
	}
}

func TestCompileClientRuntimeConfigIsBoundedAndFeatureFlagsPresent(t *testing.T) {
	svc := NewService(fakeOwnership{}, ConfigFromEnv())
	resp, err := svc.Compile(context.Background(), Input{TenantID: "tenant-1", ActorID: "actor-1"})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	rc := resp.ClientRuntimeConfig
	if rc.PageSizeDefault < minPageSizeDefault || rc.PageSizeDefault > maxPageSizeDefault {
		t.Fatalf("page size default = %d out of bounds [%d,%d]", rc.PageSizeDefault, minPageSizeDefault, maxPageSizeDefault)
	}
	if rc.SyncBackoffBaseMs < minSyncBackoffBaseMs || rc.SyncBackoffBaseMs > maxSyncBackoffBaseMs {
		t.Fatalf("sync backoff base = %d out of bounds", rc.SyncBackoffBaseMs)
	}
	if rc.JankSamplingRate < minJankSamplingRate || rc.JankSamplingRate > maxJankSamplingRate {
		t.Fatalf("jank sampling rate = %v out of bounds", rc.JankSamplingRate)
	}
	if !resp.FeatureFlags["vaccination_gaps_overlay"] || !resp.FeatureFlags["vaccination_coverage_overlay"] {
		t.Fatalf("feature flags = %#v want gaps+coverage overlay flags on", resp.FeatureFlags)
	}
	if resp.PolicyRevision == "" {
		t.Fatal("policy revision echo must not be empty")
	}
}

func TestConfigFromEnvClampsOutOfRangeOverrides(t *testing.T) {
	t.Setenv("GOATOS_APP_CONFIG_PAGE_SIZE_DEFAULT", "999999")
	t.Setenv("GOATOS_APP_CONFIG_JANK_SAMPLING_RATE", "5")
	defer os.Unsetenv("GOATOS_APP_CONFIG_PAGE_SIZE_DEFAULT")
	defer os.Unsetenv("GOATOS_APP_CONFIG_JANK_SAMPLING_RATE")

	cfg := ConfigFromEnv()
	if cfg.PageSizeDefault != maxPageSizeDefault {
		t.Fatalf("page size default = %d want clamped to %d", cfg.PageSizeDefault, maxPageSizeDefault)
	}
	if cfg.JankSamplingRate != maxJankSamplingRate {
		t.Fatalf("jank sampling rate = %v want clamped to %v", cfg.JankSamplingRate, maxJankSamplingRate)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
