package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

type fakeDiagnosisRepo struct{}

func (fakeDiagnosisRepo) SubmitObservation(ctx context.Context, in domain.SubmitObservationInput, evaluate ports.EvaluateFunc) (domain.SubmitObservationResult, error) {
	proposal, confirmable := evaluate(ctx, in.TenantID, diagnosis.Animal{
		Class:   diagnosis.ClassAdult,
		Species: "goat",
		Sex:     "F",
		Status:  "normal",
	}, in.Findings, in.Context)
	return domain.SubmitObservationResult{Proposal: proposal, Confirmable: confirmable}, nil
}

func (fakeDiagnosisRepo) ConfirmDiagnosis(context.Context, domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error) {
	return domain.ConfirmDiagnosisResult{}, nil
}
func (fakeDiagnosisRepo) GetDiagnosisRun(context.Context, string, string) (domain.DiagnosisRun, error) {
	return domain.DiagnosisRun{}, nil
}
func (fakeDiagnosisRepo) ListDiagnosisRuns(context.Context, domain.DiagnosisQueueFilter) (domain.DiagnosisQueuePage, error) {
	return domain.DiagnosisQueuePage{}, nil
}

type fakeRegisterSource struct {
	doc domain.RegisterDetail
	err error
}

func (f fakeRegisterSource) PublishedRegister(context.Context, string, string) (domain.RegisterDetail, error) {
	return f.doc, f.err
}
func (fakeRegisterSource) ListRegisters(context.Context, string) ([]domain.RegisterSummary, error) {
	return nil, nil
}
func (fakeRegisterSource) GetRegister(context.Context, string, string) (domain.RegisterDetail, error) {
	return domain.RegisterDetail{}, nil
}
func (fakeRegisterSource) GetRegisterDraftForEdit(context.Context, domain.RegisterVersionCommand, string) (domain.RegisterDetail, error) {
	return domain.RegisterDetail{}, nil
}
func (fakeRegisterSource) SaveRegisterDraft(context.Context, domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error) {
	return domain.RegisterAuthoringResult{}, nil
}
func (fakeRegisterSource) PublishRegisterDraft(context.Context, domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return domain.RegisterAuthoringResult{}, nil
}
func (fakeRegisterSource) DiscardRegisterDraft(context.Context, domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return domain.RegisterAuthoringResult{}, nil
}

func TestDiagnosisUsesPublishedTenantRegister(t *testing.T) {
	temp := 102.0
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatal(err)
	}
	doc.RegisterVersion = "tenant-live-42"

	svc, err := NewDiagnosisService(fakeDiagnosisRepo{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.WithRegisterAuthoring(fakeRegisterSource{doc: domain.RegisterDetail{Document: *doc}})

	res, err := svc.SubmitObservation(context.Background(), domain.SubmitObservationInput{
		TenantID:       "10000000-0000-4000-8000-000000000001",
		ActorID:        "20000000-0000-4000-8000-000000000001",
		GoatID:         "30000000-0000-4000-8000-000000000001",
		Findings:       diagnosis.Findings{Temp: &temp, Eating: diagnosis.MultiValue{"normal"}, Activity: "standing"},
		IdempotencyKey: "obs-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Proposal.RegisterVersion != "tenant-live-42" {
		t.Fatalf("register version = %q, want tenant-live-42; proposal=%+v", res.Proposal.RegisterVersion, res.Proposal)
	}
}

func TestDiagnosisFallsBackToSeedWhenTenantRegisterMissing(t *testing.T) {
	temp := 102.0
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.WithRegisterAuthoring(fakeRegisterSource{err: ports.ErrRegisterNotFound})

	res, err := svc.SubmitObservation(context.Background(), domain.SubmitObservationInput{
		TenantID:       "10000000-0000-4000-8000-000000000001",
		ActorID:        "20000000-0000-4000-8000-000000000001",
		GoatID:         "30000000-0000-4000-8000-000000000001",
		Findings:       diagnosis.Findings{Temp: &temp, Eating: diagnosis.MultiValue{"normal"}, Activity: "standing"},
		IdempotencyKey: "obs-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Proposal.RegisterVersion == "" || res.Proposal.RegisterVersion == "tenant-live-42" {
		t.Fatalf("fallback register version = %q", res.Proposal.RegisterVersion)
	}
}
