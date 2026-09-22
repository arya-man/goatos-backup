package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

func TestObservationFormPersistenceKeepsAuthoredAnswers(t *testing.T) {
	findings := diagnosis.Findings{Temp: ptrFloat64(104.5)}
	answers := diagnosis.Answers{"new_question": {Values: []string{"yes"}}}

	raw, err := marshalObservationForm(findings, answers)
	if err != nil {
		t.Fatalf("marshal form: %v", err)
	}

	var gotFindings diagnosis.Findings
	var gotAnswers diagnosis.Answers
	if err := decodeObservationForm(raw, &gotFindings, &gotAnswers); err != nil {
		t.Fatalf("decode form: %v", err)
	}
	if gotFindings.Temp == nil || *gotFindings.Temp != 104.5 {
		t.Fatalf("findings = %+v, want temp", gotFindings)
	}
	if gotAnswers["new_question"].Values[0] != "yes" {
		t.Fatalf("answers = %+v, want authored answer", gotAnswers)
	}
}

func TestObservationFormPersistenceReadsLegacyFindingsShape(t *testing.T) {
	raw, err := marshalObservationForm(diagnosis.Findings{Temp: ptrFloat64(103.8)}, nil)
	if err != nil {
		t.Fatalf("marshal form: %v", err)
	}
	var got diagnosis.Findings
	var answers diagnosis.Answers
	if err := decodeObservationForm(raw, &got, &answers); err != nil {
		t.Fatalf("decode form: %v", err)
	}
	if got.Temp == nil || *got.Temp != 103.8 {
		t.Fatalf("findings = %+v, want temp", got)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %+v, want none for legacy form", answers)
	}
}

func TestObservationFormPersistenceKeepsExplicitEmptyAuthoredAnswers(t *testing.T) {
	raw, err := marshalObservationForm(diagnosis.Findings{Temp: ptrFloat64(102.1)}, diagnosis.Answers{})
	if err != nil {
		t.Fatalf("marshal form: %v", err)
	}
	var got diagnosis.Findings
	var answers diagnosis.Answers
	if err := decodeObservationForm(raw, &got, &answers); err != nil {
		t.Fatalf("decode form: %v", err)
	}
	if answers == nil {
		t.Fatal("answers = nil, want present empty map")
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %+v, want empty map", answers)
	}
}

func TestDiagnosisQueueProjectionOneToManyPageBoundaryDateShiftParkScopeStatusMatrix(t *testing.T) {
	sql := diagnosisRunsPageSQL
	for _, want := range []string{
		"JOIN goats g ON g.tenant_id = dr.tenant_id AND g.goat_id = dr.goat_id",
		"dr.tenant_id = $1::uuid",
		"dr.status = $2",
		"(dr.observed_at, dr.health_diagnosis_run_id)",
		"ORDER BY dr.observed_at DESC, dr.health_diagnosis_run_id DESC",
		"LIMIT $6",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("diagnosis queue SQL missing %q", want)
		}
	}
	if strings.Contains(sql, " OFFSET ") {
		t.Fatalf("diagnosis queue SQL must stay keyset paged, got OFFSET in:\n%s", sql)
	}
}

func ptrFloat64(v float64) *float64 { return &v }
