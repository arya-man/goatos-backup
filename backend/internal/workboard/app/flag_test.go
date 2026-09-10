package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

type fakeRaiser struct{ last ltports.RaiseParams }

func (f *fakeRaiser) Raise(_ context.Context, p ltports.RaiseParams) (ltdomain.Task, error) {
	f.last = p
	return ltdomain.Task{TaskID: "t", TaskNo: 7, AssigneeUserID: p.AssigneeUserID}, nil
}

type fakeHeads struct {
	head ports.ParkHead
	err  error
}

func (f *fakeHeads) ParkHead(context.Context, string, string) (ports.ParkHead, error) {
	return f.head, f.err
}

func TestFlagComposesTheBriefForTheParkHead(t *testing.T) {
	raiser := &fakeRaiser{}
	svc := NewFlagService(raiser, &fakeHeads{head: ports.ParkHead{UserID: "u-head", Name: "Naveen R."}})
	res, err := svc.Flag(context.Background(), ports.FlagParams{
		TenantID: "t", ActorID: "u-dir", ActorDesignation: "feed_director", ParkID: "p",
		RowKey: "feed|feed_transport_task|x", RowTitle: "Transport Castro 1", RowSubtitle: "One trip · stage by 15:00",
		PenDisplay: "Castro 1", ClockLabel: "Stage by 15:00", Note: "Bags still not staged", IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskNo != 7 || res.AssigneeName != "Naveen R." {
		t.Fatalf("result %+v", res)
	}
	if raiser.last.AssigneeUserID != "u-head" || raiser.last.ActorID != "u-dir" || raiser.last.ActorDesignation != "feed_director" || raiser.last.IdempotencyKey != "k" {
		t.Fatalf("raise params %+v", raiser.last)
	}
	if raiser.last.Title != "Check · Transport Castro 1" {
		t.Fatalf("title %q", raiser.last.Title)
	}
	for _, want := range []string{"One trip · stage by 15:00", "Castro 1 · Stage by 15:00", "Bags still not staged", "Flagged from the Work Board · feed|feed_transport_task|x"} {
		if !strings.Contains(raiser.last.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, raiser.last.Body)
		}
	}
	if len([]rune(FlagTitle(strings.Repeat("x", 200)))) > 80 {
		t.Fatal("title must stay inside the Leadership Tasks limit")
	}
}

func TestFlagRefusesWithoutAParkHeadOrARow(t *testing.T) {
	svc := NewFlagService(&fakeRaiser{}, &fakeHeads{err: ports.ErrParkHeadMissing})
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", ParkID: "p", RowKey: "k", RowTitle: "t", IdempotencyKey: "k"}); !errors.Is(err, ports.ErrParkHeadMissing) {
		t.Fatalf("expected ErrParkHeadMissing, got %v", err)
	}
	svc = NewFlagService(&fakeRaiser{}, &fakeHeads{head: ports.ParkHead{UserID: "u"}})
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", ParkID: "p", IdempotencyKey: "k"}); !errors.Is(err, ErrFlagRowRequired) {
		t.Fatalf("expected ErrFlagRowRequired, got %v", err)
	}
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", ParkID: "p", RowKey: "k", RowTitle: "t", Note: strings.Repeat("n", 1001), IdempotencyKey: "k"}); !errors.Is(err, ErrFlagNoteTooLong) {
		t.Fatalf("expected ErrFlagNoteTooLong, got %v", err)
	}
}
