package main

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const testTenantID = "00000000-0000-4000-8000-000000000001"

func TestParseConfigRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	_, err := parseConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("parseConfig() err = %v, want tenant-id required", err)
	}
}

func TestParseConfigDefaultsSourceDateToYesterday(t *testing.T) {
	now := time.Now().In(biztime.DefaultLocation())
	want := biztime.BusinessDayStart(now.AddDate(0, 0, -1)).Format("2006-01-02")

	cfg, err := parseConfig([]string{"-tenant-id", testTenantID})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if got := cfg.SourceDate.Format("2006-01-02"); got != want {
		t.Fatalf("SourceDate = %q, want %q (yesterday)", got, want)
	}
	if cfg.eventDateSuffix() != strings.ReplaceAll(want, "-", "") {
		t.Fatalf("eventDateSuffix() = %q, want %q", cfg.eventDateSuffix(), strings.ReplaceAll(want, "-", ""))
	}
}

func TestParseConfigRejectsBadSourceDate(t *testing.T) {
	_, err := parseConfig([]string{"-tenant-id", testTenantID, "-source-date", "not-a-date"})
	if err == nil || !strings.Contains(err.Error(), "source-date must be YYYY-MM-DD") {
		t.Fatalf("parseConfig() err = %v, want source-date format error", err)
	}
}

func TestParseConfigHonorsExplicitSourceDate(t *testing.T) {
	cfg, err := parseConfig([]string{"-tenant-id", testTenantID, "-source-date", "2026-07-01"})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if got := cfg.SourceDate.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("SourceDate = %q, want 2026-07-01", got)
	}
	if cfg.eventDateSuffix() != "20260701" {
		t.Fatalf("eventDateSuffix() = %q, want 20260701", cfg.eventDateSuffix())
	}
}

func TestParseConfigRejectsNonPositiveMaxBytesBilled(t *testing.T) {
	_, err := parseConfig([]string{"-tenant-id", testTenantID, "-max-bytes-billed", "0"})
	if err == nil || !strings.Contains(err.Error(), "max-bytes-billed must be positive") {
		t.Fatalf("parseConfig() err = %v, want max-bytes-billed error", err)
	}
}

func TestParseConfigDefaultsGA4ProjectToGoogleCloudProject(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "goatos-stg")
	t.Setenv("GOATOS_GA4_BQ_PROJECT", "")
	cfg, err := parseConfig([]string{"-tenant-id", testTenantID})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if cfg.GA4BQProject != "goatos-stg" {
		t.Fatalf("GA4BQProject = %q, want goatos-stg (fallback to GOOGLE_CLOUD_PROJECT)", cfg.GA4BQProject)
	}
}

func TestParseConfigGA4DatasetEmptyMeansNotLinkedYet(t *testing.T) {
	t.Setenv("GOATOS_GA4_EXPORT_DATASET", "")
	t.Setenv("GOATOS_GA4_BQ_DATASET", "")
	cfg, err := parseConfig([]string{"-tenant-id", testTenantID})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if cfg.GA4Dataset != "" {
		t.Fatalf("GA4Dataset = %q, want empty (GA4 not linked)", cfg.GA4Dataset)
	}
}

func TestParseConfigGA4ExportDatasetEnvWinsOverTaskAliasEnv(t *testing.T) {
	t.Setenv("GOATOS_GA4_EXPORT_DATASET", "analytics_infra_dataset")
	t.Setenv("GOATOS_GA4_BQ_DATASET", "analytics_task_alias_dataset")
	cfg, err := parseConfig([]string{"-tenant-id", testTenantID})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if cfg.GA4Dataset != "analytics_infra_dataset" {
		t.Fatalf("GA4Dataset = %q, want infra's GOATOS_GA4_EXPORT_DATASET to win", cfg.GA4Dataset)
	}
}

func TestParseConfigDefaultBQLocationIsAsiaSouth1(t *testing.T) {
	t.Setenv("GOATOS_BQ_LOCATION", "")
	cfg, err := parseConfig([]string{"-tenant-id", testTenantID})
	if err != nil {
		t.Fatalf("parseConfig() err = %v", err)
	}
	if cfg.BQLocation != defaultBQLocation {
		t.Fatalf("BQLocation = %q, want %q", cfg.BQLocation, defaultBQLocation)
	}
}

func TestDefaultFunnelStepsAreOrderedAndNonEmpty(t *testing.T) {
	steps := defaultFunnelSteps()
	if len(steps) < 2 {
		t.Fatalf("defaultFunnelSteps() len = %d, want >= 2", len(steps))
	}
	for i, step := range steps {
		if step.StepIndex != i {
			t.Fatalf("step %d (%s) has StepIndex %d, want %d", i, step.StepKey, step.StepIndex, i)
		}
		if step.EventName == "" || step.StepKey == "" || step.FunnelKey == "" {
			t.Fatalf("step %d has an empty required field: %+v", i, step)
		}
	}
}

func TestCrashFreeRatioPct(t *testing.T) {
	cases := []struct {
		name       string
		crashCount int64
		total      int64
		want       float64
	}{
		{"no denominator treated as fully crash-free", 5, 0, 100},
		{"zero crashes is 100pct", 0, 1000, 100},
		{"half crashed clamps within range", 500, 1000, 50},
		{"crash count exceeding total clamps at zero", 2000, 1000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := crashFreeRatioPct(tc.crashCount, tc.total); got != tc.want {
				t.Fatalf("crashFreeRatioPct(%d, %d) = %v, want %v", tc.crashCount, tc.total, got, tc.want)
			}
		})
	}
}
