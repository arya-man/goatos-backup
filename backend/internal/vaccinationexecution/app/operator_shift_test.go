package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// fakeRepo's base answers for the operator-shift write: nothing is written.
func (r fakeRepo) SetOperatorShift(_ context.Context, w ports.OperatorShiftWrite) (domain.OperatorShift, bool, error) {
	return w.Shift, false, r.err
}

func (r fakeRepo) ClearOperatorShift(_ context.Context, _ ports.OperatorShiftClear) (bool, error) {
	return false, r.err
}

type operatorShiftRecordingRepo struct {
	fakeRepo
	writes []ports.OperatorShiftWrite
}

func (r *operatorShiftRecordingRepo) SetOperatorShift(_ context.Context, w ports.OperatorShiftWrite) (domain.OperatorShift, bool, error) {
	r.writes = append(r.writes, w)
	return w.Shift, false, nil
}

func TestSetOperatorShiftRefusesABadFieldBeforeAnyWrite(t *testing.T) {
	repo := &operatorShiftRecordingRepo{}
	svc := NewService(repo)
	_, _, ferr, err := svc.SetOperatorShift(context.Background(), OperatorShiftRequest{TenantID: "t", IdempotencyKey: "k"}, domain.OperatorShiftInput{
		ParkID: "p", OperatorID: "o", ShiftLabel: "am", ShiftStart: "17:00", ShiftEnd: "08:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ferr == nil || ferr.Field != "shift_end" {
		t.Fatalf("want a shift_end refusal, got %+v", ferr)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("a refused shift must not reach the repository, got %d writes", len(repo.writes))
	}
}

func TestSetOperatorShiftWritesTheParsedMinutes(t *testing.T) {
	repo := &operatorShiftRecordingRepo{}
	svc := NewService(repo)
	weekOff := "sunday"
	shift, replay, ferr, err := svc.SetOperatorShift(context.Background(), OperatorShiftRequest{TenantID: "t", ActorID: "a", IdempotencyKey: "k"}, domain.OperatorShiftInput{
		ParkID: "p", OperatorID: "o", ShiftLabel: "pm", ShiftStart: "13:30", ShiftEnd: "21:00", WeekOffWeekday: &weekOff,
	})
	if err != nil || ferr != nil || replay {
		t.Fatalf("err=%v ferr=%+v replay=%v", err, ferr, replay)
	}
	if len(repo.writes) != 1 {
		t.Fatalf("want one write, got %d", len(repo.writes))
	}
	w := repo.writes[0]
	if w.IdempotencyKey != "k" || w.ActorID != "a" || w.Shift.ShiftStartMinute != 810 || w.Shift.ShiftEndMinute != 1260 || w.Shift.WeekOffWeekday != "sunday" || w.Shift.ParkID != "p" {
		t.Fatalf("unexpected write: %+v", w)
	}
	if shift.ShiftLabel != "pm" {
		t.Fatalf("unexpected returned shift: %+v", shift)
	}
}
