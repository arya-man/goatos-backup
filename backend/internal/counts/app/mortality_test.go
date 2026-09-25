package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

type mortalityFakeRepo struct {
	fakeRepo
	out domain.Mortality
}

func (f *mortalityFakeRepo) GetMortality(context.Context, domain.MortalityQuery) (domain.Mortality, error) {
	return f.out, nil
}

var _ ports.MortalityRepository = (*mortalityFakeRepo)(nil)

type fakeCauseLabeler struct{ labels map[string]string }

func (f fakeCauseLabeler) LabelDeathCause(_ context.Context, _ string, key string) string {
	return f.labels[key]
}

// A repo with no mortality capability must fail CLOSED: an empty payload would render as a
// farm where nothing dies, which is a different claim from "this read is not wired here".
func TestGetMortalityFailsClosedWithoutAReader(t *testing.T) {
	svc := NewHerdRegisterService(&fakeRepo{})
	if _, err := svc.GetMortality(context.Background(), domain.MortalityQuery{TenantID: "t"}); !errors.Is(err, ErrMortalityUnavailable) {
		t.Fatalf("err=%v, want ErrMortalityUnavailable", err)
	}
}

// The adapter hands a RECORDED cause up as its raw register key with an empty label; the
// service names it through Health's vocabulary, leaves an INFERRED cause's own disease name
// alone, gives the no-cause bucket the domain's one owned label -- and does the same on the
// cross-tab cause column and the recent list, so the board never shows a register key.
func TestGetMortalityNamesRecordedCausesThroughHealth(t *testing.T) {
	repo := &mortalityFakeRepo{out: domain.Mortality{
		Cause: []domain.MortalityBucket{
			{Key: "MASTITIS", Basis: domain.MortalityCauseRecorded, Deaths: 2},
			{Key: "inferred:Pneumonia", Label: "Pneumonia", Basis: domain.MortalityCauseInferred, Deaths: 1},
			{Key: "", Basis: domain.MortalityCauseNone, Deaths: 3},
		},
		BreedByCause: []domain.MortalityCrossCell{
			{RowKey: "Beetal", RowLabel: "Beetal", ColKey: "MASTITIS", Deaths: 2},
			{RowKey: "Beetal", RowLabel: "Beetal", ColKey: "", Deaths: 3},
		},
		Deaths: []domain.MortalityDeath{
			{GoatID: "a", CauseKey: "MASTITIS", CauseBasis: domain.MortalityCauseRecorded},
			{GoatID: "b", CauseBasis: domain.MortalityCauseNone},
			{GoatID: "c", CauseLabel: "Pneumonia", CauseBasis: domain.MortalityCauseInferred},
		},
	}}
	svc := NewHerdRegisterService(repo).WithDeathCauseLabeler(fakeCauseLabeler{labels: map[string]string{"MASTITIS": "Mastitis"}})
	out, err := svc.GetMortality(context.Background(), domain.MortalityQuery{TenantID: "t"})
	if err != nil {
		t.Fatalf("GetMortality: %v", err)
	}
	if out.Cause[0].Label != "Mastitis" || out.Cause[1].Label != "Pneumonia" || out.Cause[2].Label != domain.MortalityCauseNoneLabel {
		t.Fatalf("cause labels %+v", out.Cause)
	}
	if out.BreedByCause[0].ColLabel != "Mastitis" || out.BreedByCause[1].ColLabel != domain.MortalityCauseNoneLabel {
		t.Fatalf("cross labels %+v", out.BreedByCause)
	}
	if out.Deaths[0].CauseLabel != "Mastitis" || out.Deaths[1].CauseLabel != domain.MortalityCauseNoneLabel || out.Deaths[2].CauseLabel != "Pneumonia" {
		t.Fatalf("recent labels %+v", out.Deaths)
	}
}

// Without Health wired in, a recorded key still shows AS the key rather than vanishing: a
// cause the farm recorded must never read as "no cause".
func TestGetMortalityKeepsTheKeyWhenNoLabelerIsWired(t *testing.T) {
	repo := &mortalityFakeRepo{out: domain.Mortality{Cause: []domain.MortalityBucket{{Key: "MASTITIS", Basis: domain.MortalityCauseRecorded, Deaths: 1}}}}
	out, err := NewHerdRegisterService(repo).GetMortality(context.Background(), domain.MortalityQuery{TenantID: "t"})
	if err != nil {
		t.Fatalf("GetMortality: %v", err)
	}
	if out.Cause[0].Label != "MASTITIS" {
		t.Fatalf("label=%q want the raw key", out.Cause[0].Label)
	}
}
