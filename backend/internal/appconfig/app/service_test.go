package app

import (
	"context"
	"os"
	"testing"
)

func TestCompileIsDeterministicForSameInput(t *testing.T) {
	svc := NewService(ConfigFromEnv())

	first, err := svc.Compile(context.Background())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	second, err := svc.Compile(context.Background())
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

func TestCompileClientRuntimeConfigIsBoundedAndFeatureFlagsPresent(t *testing.T) {
	svc := NewService(ConfigFromEnv())
	resp, err := svc.Compile(context.Background())
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
