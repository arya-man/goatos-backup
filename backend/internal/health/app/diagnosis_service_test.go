package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

type fakeDiagnosisRepo struct{}

func (fakeDiagnosisRepo) SubmitObservation(context.Context, domain.SubmitObservationInput, ports.EvaluateFunc) (domain.SubmitObservationResult, error) {
	return domain.SubmitObservationResult{}, nil
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

func TestDiagnosisEvaluationUsesPublishedAuthoredRegister(t *testing.T) {
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatalf("seed authored register: %v", err)
	}
	doc.RegisterVersion = "adult-live-from-health-config"
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{})
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}

	proposal, _, err := svc.evaluate(context.Background(), doc,
		diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F", Status: "normal"},
		diagnosis.Findings{Temp: ptrFloat(104.5)},
		nil,
		diagnosis.Context{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if proposal.RegisterVersion != "adult-live-from-health-config" {
		t.Fatalf("register version = %q, want the published authored register", proposal.RegisterVersion)
	}
}

func TestDiagnosisEvaluationFallsBackToCommittedSeedWhenNoPublishedRegisterExists(t *testing.T) {
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{})
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}

	proposal, _, err := svc.evaluate(context.Background(), nil,
		diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F", Status: "normal"},
		diagnosis.Findings{Temp: ptrFloat(104.5)},
		nil,
		diagnosis.Context{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if proposal.RegisterVersion != "adult-1" {
		t.Fatalf("register version = %q, want committed fallback adult-1", proposal.RegisterVersion)
	}
}

func TestDiagnosisEvaluationReturnsPublishedRegisterCompileErrors(t *testing.T) {
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatalf("seed authored register: %v", err)
	}
	doc.AppliesClass = []string{diagnosis.ClassKidMilk}
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{})
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}

	_, _, err = svc.evaluate(context.Background(), doc,
		diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F", Status: "normal"},
		diagnosis.Findings{Temp: ptrFloat(104.5)},
		nil,
		diagnosis.Context{})

	if err == nil || !strings.Contains(err.Error(), "does not claim class adult") {
		t.Fatalf("err = %v, want compile error", err)
	}
}

func TestDiagnosisEvaluationAcceptsAuthoredAnswers(t *testing.T) {
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatalf("seed authored register: %v", err)
	}
	doc.RegisterVersion = "adult-authored-answers"
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{})
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}
	animal := diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F", Status: "normal"}
	findings := diagnosis.Findings{Temp: ptrFloat(104.5)}

	proposal, _, err := svc.evaluate(context.Background(), doc, animal, findings, diagnosis.LegacyAnswers(animal, findings), diagnosis.Context{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if proposal.RegisterVersion != "adult-authored-answers" {
		t.Fatalf("register version = %q, want authored register", proposal.RegisterVersion)
	}
}

func TestDiagnosisEvaluationRejectsExplicitEmptyAuthoredAnswers(t *testing.T) {
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatalf("seed authored register: %v", err)
	}
	svc, err := NewDiagnosisService(fakeDiagnosisRepo{})
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}

	proposal, _, err := svc.evaluate(context.Background(), doc,
		diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F", Status: "normal"},
		diagnosis.Findings{},
		diagnosis.Answers{},
		diagnosis.Context{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if proposal.Valid || !strings.Contains(proposal.RejectReason, "has not been answered") {
		t.Fatalf("proposal = %+v, want authored validation rejection", proposal)
	}
}

func ptrFloat(v float64) *float64 { return &v }
