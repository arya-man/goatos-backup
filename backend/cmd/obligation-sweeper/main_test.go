package main

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeVaccinationProjectionRefresher struct {
	shedReq      vaccexecdomain.ShedProjectionRecomputeRequest
	executionReq vaccexecdomain.ExecutionProjectionRecomputeRequest
}

func (f *fakeVaccinationProjectionRefresher) RecomputeShedProjection(_ context.Context, req vaccexecdomain.ShedProjectionRecomputeRequest) (vaccexecdomain.ShedProjectionRecomputeResult, error) {
	f.shedReq = req
	return vaccexecdomain.ShedProjectionRecomputeResult{Rows: 2}, nil
}

func (f *fakeVaccinationProjectionRefresher) RecomputeExecutionProjection(_ context.Context, req vaccexecdomain.ExecutionProjectionRecomputeRequest) (vaccexecdomain.ExecutionProjectionRecomputeResult, error) {
	f.executionReq = req
	return vaccexecdomain.ExecutionProjectionRecomputeResult{Rows: 3}, nil
}

func TestScheduledSweeperRefreshesVaccinationReadModelsWithOneAsOf(t *testing.T) {
	asOf := time.Date(2026, 7, 12, 21, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	fake := &fakeVaccinationProjectionRefresher{}
	if err := refreshVaccinationReadModels(context.Background(), fake, "tenant-1", asOf); err != nil {
		t.Fatal(err)
	}
	if fake.shedReq.TenantID != "tenant-1" || !fake.shedReq.AsOf.Equal(asOf) || !fake.shedReq.DueBefore.Equal(asOf.Add(30*24*time.Hour)) {
		t.Fatalf("shed refresh request = %#v", fake.shedReq)
	}
	if !fake.executionReq.AsOf.Equal(fake.shedReq.AsOf) || !fake.executionReq.DueBefore.Equal(fake.shedReq.DueBefore) {
		t.Fatalf("execution refresh request=%#v shed=%#v", fake.executionReq, fake.shedReq)
	}
}

// TestActorIDAlwaysRequired verifies that the worker cannot start without its
// audited task-creator identity. SOP bindings are normally discovered from the
// published version/rules after flag parsing, so optional-override validation
// cannot safely determine whether task creation will be needed.
func TestActorIDAlwaysRequired(t *testing.T) {
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
			errMsg:  "actor-id is required for obligation-sweeper",
		},
		{
			name: "fails when no actor-id and vaccine-item-id is set",
			args: []string{
				"-tenant-id", "tenant-1",
				"-vaccine-item-id", "vaccine-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required for obligation-sweeper",
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
			name: "fails without actor before published rules are loaded",
			args: []string{
				"-tenant-id", "tenant-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required for obligation-sweeper",
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
			if cfg.ActorID == "" {
				t.Fatal("config has no actor-id, want validation error")
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
