package main

import (
	"strings"
	"testing"
	"time"
)

const testTenantID = "00000000-0000-4000-8000-000000000001"

func TestParseFlags(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) }
	cfg, err := parseFlags([]string{
		"-tenant-id", testTenantID,
		"-limit", "250",
		"-before", "2026-06-27T00:00:00Z",
		"-dry-run",
	}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.TenantID != testTenantID || cfg.Limit != 250 || !cfg.DryRun {
		t.Fatalf("cfg = %#v", cfg)
	}
	wantBefore := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	if !cfg.Before.Equal(wantBefore) {
		t.Fatalf("Before = %s, want %s", cfg.Before, wantBefore)
	}
}

func TestParseFlagsDefaultsBeforeFromRetention(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) }
	cfg, err := parseFlags([]string{"-retention", "48h"}, now)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	wantBefore := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	if !cfg.Before.Equal(wantBefore) {
		t.Fatalf("Before = %s, want %s", cfg.Before, wantBefore)
	}
}

func TestParseFlagsRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "limit too low", args: []string{"-limit", "0"}, want: "limit"},
		{name: "limit too high", args: []string{"-limit", "5001"}, want: "limit"},
		{name: "bad before", args: []string{"-before", "today"}, want: "before"},
		{name: "bad tenant", args: []string{"-tenant-id", "not-a-uuid"}, want: "tenant-id"},
		{name: "bad retention", args: []string{"-retention", "0s"}, want: "retention"},
		{name: "bad timeout", args: []string{"-timeout", "0s"}, want: "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFlags(tt.args, func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}
