package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

type fakePublishedRegister struct {
	detail domain.RegisterDetail
	err    error
}

func (f fakePublishedRegister) PublishedRegister(context.Context, string, string) (domain.RegisterDetail, error) {
	return f.detail, f.err
}

func TestDiagnosisServiceUsesPublishedAuthoredRegister(t *testing.T) {
	doc, err := diagnosis.SeedAuthored(diagnosis.ClassAdult, domain.SOPRefToDiseaseKey)
	if err != nil {
		t.Fatalf("seed authored register: %v", err)
	}
	doc.RegisterVersion = "adult-authored-test"

	svc := &DiagnosisService{
		registers: map[string]*diagnosis.Register{},
		publishedRegister: fakePublishedRegister{detail: domain.RegisterDetail{
			RegisterSummary: domain.RegisterSummary{AnimalClass: diagnosis.ClassAdult},
			Document:        *doc,
		}},
	}

	reg, err := svc.registerFor(context.Background(), "tenant-1", diagnosis.ClassAdult)
	if err != nil {
		t.Fatalf("registerFor: %v", err)
	}
	if reg.Version != "adult-authored-test" {
		t.Fatalf("register version = %q, want the published authored version", reg.Version)
	}
	if reg.BoundClass() != diagnosis.ClassAdult {
		t.Fatalf("bound class = %q, want %q", reg.BoundClass(), diagnosis.ClassAdult)
	}
}

func TestDiagnosisServiceFallsBackOnlyWhenNoPublishedRegisterExists(t *testing.T) {
	embedded, err := diagnosis.RegisterFor(diagnosis.ClassAdult)
	if err != nil {
		t.Fatalf("embedded register: %v", err)
	}
	svc := &DiagnosisService{
		registers:         map[string]*diagnosis.Register{diagnosis.ClassAdult: embedded},
		publishedRegister: fakePublishedRegister{err: ports.ErrRegisterNotFound},
	}

	reg, err := svc.registerFor(context.Background(), "tenant-1", diagnosis.ClassAdult)
	if err != nil {
		t.Fatalf("registerFor fallback: %v", err)
	}
	if reg.Version != embedded.Version {
		t.Fatalf("fallback version = %q, want %q", reg.Version, embedded.Version)
	}
}

func TestDiagnosisServiceFailsClosedWhenPublishedRegisterReadFails(t *testing.T) {
	embedded, err := diagnosis.RegisterFor(diagnosis.ClassAdult)
	if err != nil {
		t.Fatalf("embedded register: %v", err)
	}
	svc := &DiagnosisService{
		registers:         map[string]*diagnosis.Register{diagnosis.ClassAdult: embedded},
		publishedRegister: fakePublishedRegister{err: errors.New("database offline")},
	}

	if _, err := svc.registerFor(context.Background(), "tenant-1", diagnosis.ClassAdult); err == nil {
		t.Fatal("registerFor must fail closed on a published-register read error")
	}
}
