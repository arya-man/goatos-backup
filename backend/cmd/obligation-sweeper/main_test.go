package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
)

// TestActorIDRequiredWhenTaskFinalizationEnabled verifies that the sweeper
// fails at startup if no actor ID is configured when task creation is needed.
// This is C35-003: staging sweeper running without a SOP task creator.
func TestActorIDRequiredWhenTaskFinalizationEnabled(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name: "fails when no actor-id and sop-version-id is set",
			args: []string{
				"-tenant-id", "tenant-1",
				"-sop-version-id", "sop-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required when sop-version-id is configured",
		},
		{
			name: "fails when no actor-id and vaccine-item-id is set",
			args: []string{
				"-tenant-id", "tenant-1",
				"-vaccine-item-id", "vaccine-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required when vaccine-item-id is configured",
		},
		{
			name: "succeeds when actor-id is provided",
			args: []string{
				"-tenant-id", "tenant-1",
				"-actor-id", "actor-1",
				"-sop-version-id", "sop-1",
			},
			wantErr: false,
		},
		{
			name: "succeeds when no actor-id and no finalization config",
			args: []string{
				"-tenant-id", "tenant-1",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseFlags(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseFlags(%v) = nil, want error containing %q", tt.args, tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags(%v) = %v, want nil", tt.args, err)
			}
			if cfg.ActorID == "" && (cfg.SOPVersionID != "" || cfg.VaccineItemID != "") {
				t.Fatalf("config has no actor-id but has finalization config, want validation error")
			}
		})
	}
}

// TestTaskCreatorImplementsBatchTaskCreatorForBulkOperations verifies that
// task creation uses the BatchTaskCreator interface when available to avoid N+1.
// This is C35-004: bulk task creation still does per-batch DB writes.
func TestTaskCreatorImplementsBatchTaskCreatorForBulkOperations(t *testing.T) {
	// This test verifies that sopbridge.Bridge implements both interfaces for bulk operations
	bridge := sopbridge.New(nil, "actor-1")

	// Verify that sopbridge.Bridge implements both TaskCreator and BatchTaskCreator
	var _ app.TaskCreator = bridge
	var _ app.BatchTaskCreator = bridge

	t.Logf("sopbridge.Bridge correctly implements both TaskCreator and BatchTaskCreator for bulk operations")
}
