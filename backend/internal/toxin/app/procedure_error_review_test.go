package app

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
	"testing"
)

type unavailableProcedureSource struct {
	fakeProcedures
	err error
}

func (s unavailableProcedureSource) ProcedureVersion(context.Context, string, int) (domain.Procedure, error) {
	return domain.Procedure{}, s.err
}
func TestConfiguredProcedureFailureNeverReturnsSeed(t *testing.T) {
	unavailable := errors.New("procedure database unavailable")
	repo := &versionedRepo{version: 2}
	svc := NewService(repo, &fakeProofs{}).WithProcedureSource(&unavailableProcedureSource{err: unavailable})
	row, err := svc.GetTask(context.Background(), "t1", "task-1")
	if !errors.Is(err, unavailable) {
		t.Fatalf("read returned procedure v%d instead of source error: %v", row.Procedure.Version, err)
	}
	_, err = svc.CompleteStep(context.Background(), ports.CompleteStepParams{TenantID: "t1", TaskID: "task-1", StepNo: 2, ProofRef: "proof-1", IdempotencyKey: "key"})
	if !errors.Is(err, unavailable) || repo.completeStepCalls != 0 {
		t.Fatalf("step err=%v writes=%d", err, repo.completeStepCalls)
	}
	_, err = svc.SubmitReading(context.Background(), ports.SubmitParams{TenantID: "t1", TaskID: "task-1", Outcome: domain.OutcomeNegative, StripPhotoRef: "proof-1", IdempotencyKey: "reading"})
	if !errors.Is(err, unavailable) || repo.submitCalls != 0 {
		t.Fatalf("reading err=%v writes=%d", err, repo.submitCalls)
	}
	_, err = svc.RecordVerdict(context.Background(), ports.VerdictParams{TenantID: "t1", TaskID: "task-1", Decision: "accept", IdempotencyKey: "verdict"})
	if !errors.Is(err, unavailable) || repo.verdictCalls != 0 {
		t.Fatalf("verdict err=%v writes=%d", err, repo.verdictCalls)
	}
	cache := map[int]domain.Procedure{}
	if _, err := svc.procedureFor(context.Background(), "t1", 2, cache); !errors.Is(err, unavailable) || len(cache) != 0 {
		t.Fatalf("source failure poisoned cache: %v %v", cache, err)
	}

}
