package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
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

// fakeFinder answers with one row for one key and records the board it was asked about.
type fakeFinder struct {
	row   domain.Row
	key   string
	board domain.Query
	err   error
}

func (f *fakeFinder) FindRow(_ context.Context, q domain.Query, key string) (domain.Row, bool, error) {
	f.board = q
	if f.err != nil {
		return domain.Row{}, false, f.err
	}
	if key != f.key {
		return domain.Row{}, false, nil
	}
	return f.row, true, nil
}

func transportRow() domain.Row {
	return domain.Row{
		Module: domain.ModuleFeed, SourceType: "feed_transport_task", SourceID: "x",
		ParkID: "p", BusinessDate: "2026-09-10", Title: "Transport Castro 1",
		Subtitle: "One trip · stage by 15:00", ClockLabel: "Stage by 15:00",
		Pen: domain.Pen{Display: "Castro 1"}, WorkState: domain.WorkStateDue,
	}.Finalize()
}

func board() domain.Query {
	return domain.Query{TenantID: "t", ParkID: "p", BusinessDate: "2026-09-10", Modules: []domain.Module{domain.ModuleFeed}}
}

// TestFlagComposesTheBriefFromTheRowTheBoardFound: title, pen, clock and subtitle come from
// the row the board resolved, the note from the director, and the body names no row key.
func TestFlagComposesTheBriefFromTheRowTheBoardFound(t *testing.T) {
	raiser := &fakeRaiser{}
	finder := &fakeFinder{row: transportRow(), key: "feed|feed_transport_task|x"}
	svc := NewFlagService(finder, raiser, &fakeHeads{head: ports.ParkHead{UserID: "u-head", Name: "Naveen R."}})
	res, err := svc.Flag(context.Background(), ports.FlagParams{
		TenantID: "t", ActorID: "u-dir", ActorDesignation: "feed_director", Board: board(),
		RowKey: "feed|feed_transport_task|x", Note: "Bags still not staged", IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskNo != 7 || res.AssigneeName != "Naveen R." {
		t.Fatalf("result %+v", res)
	}
	if finder.board.ParkID != "p" || finder.board.BusinessDate != "2026-09-10" || len(finder.board.Modules) != 1 {
		t.Fatalf("the row must be looked up on the caller's own board, got %+v", finder.board)
	}
	if raiser.last.AssigneeUserID != "u-head" || raiser.last.ActorID != "u-dir" || raiser.last.ActorDesignation != "feed_director" || raiser.last.IdempotencyKey != "k" {
		t.Fatalf("raise params %+v", raiser.last)
	}
	if raiser.last.Title != "Check · Transport Castro 1" {
		t.Fatalf("title %q", raiser.last.Title)
	}
	for _, want := range []string{"One trip · stage by 15:00", "Castro 1 · Stage by 15:00", "Bags still not staged", "Flagged from the Work Board · 10/09/2026"} {
		if !strings.Contains(raiser.last.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, raiser.last.Body)
		}
	}
	// Copy firewall: the park head reads this on a phone. No key, no id, no module token.
	for _, banned := range []string{"feed_transport_task", "feed|", "|x"} {
		if strings.Contains(raiser.last.Body, banned) {
			t.Errorf("body leaks %q:\n%s", banned, raiser.last.Body)
		}
	}
	if len([]rune(FlagTitle(strings.Repeat("x", 200)))) > 80 {
		t.Fatal("title must stay inside the Leadership Tasks limit")
	}
}

// TestFlagRefusesARowTheCallerCannotSee: a key the caller's board does not hold (another
// park, another day, a module outside their visibility, or nothing at all) raises nothing.
func TestFlagRefusesARowTheCallerCannotSee(t *testing.T) {
	raiser := &fakeRaiser{}
	svc := NewFlagService(&fakeFinder{row: transportRow(), key: "feed|feed_transport_task|x"}, raiser, &fakeHeads{head: ports.ParkHead{UserID: "u"}})
	_, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", Board: board(), RowKey: "weighing|weighing_work_item|w9", IdempotencyKey: "k"})
	if !errors.Is(err, ErrFlagRowNotFound) {
		t.Fatalf("expected ErrFlagRowNotFound, got %v", err)
	}
	if raiser.last.IdempotencyKey != "" {
		t.Fatal("nothing may be raised for a row the board did not find")
	}
	svc = NewFlagService(&fakeFinder{err: domain.ErrInvalidRowKey}, raiser, &fakeHeads{head: ports.ParkHead{UserID: "u"}})
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", Board: board(), RowKey: "garbage", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrInvalidRowKey) {
		t.Fatalf("expected ErrInvalidRowKey, got %v", err)
	}
}

func TestFlagRefusesWithoutAParkHeadOrARow(t *testing.T) {
	finder := &fakeFinder{row: transportRow(), key: "feed|feed_transport_task|x"}
	svc := NewFlagService(finder, &fakeRaiser{}, &fakeHeads{err: ports.ErrParkHeadMissing})
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", Board: board(), RowKey: "feed|feed_transport_task|x", IdempotencyKey: "k"}); !errors.Is(err, ports.ErrParkHeadMissing) {
		t.Fatalf("expected ErrParkHeadMissing, got %v", err)
	}
	svc = NewFlagService(finder, &fakeRaiser{}, &fakeHeads{head: ports.ParkHead{UserID: "u"}})
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", Board: board(), IdempotencyKey: "k"}); !errors.Is(err, ErrFlagRowRequired) {
		t.Fatalf("expected ErrFlagRowRequired, got %v", err)
	}
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", RowKey: "feed|feed_transport_task|x", IdempotencyKey: "k"}); !errors.Is(err, ErrFlagParkRequired) {
		t.Fatalf("expected ErrFlagParkRequired, got %v", err)
	}
	if _, err := svc.Flag(context.Background(), ports.FlagParams{TenantID: "t", Board: board(), RowKey: "feed|feed_transport_task|x", Note: strings.Repeat("n", 1001), IdempotencyKey: "k"}); !errors.Is(err, ErrFlagNoteTooLong) {
		t.Fatalf("expected ErrFlagNoteTooLong, got %v", err)
	}
}
