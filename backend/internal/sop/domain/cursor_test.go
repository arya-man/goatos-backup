package domain

import (
	"testing"
	"time"
)

func TestSOPCursorRoundTripAndRejectsMalformed(t *testing.T) {
	want := SOPCursor{UpdatedAt: time.Date(2026, 7, 12, 0, 0, 0, 123, time.UTC), SOPID: "10000000-0000-4000-8000-000000000001"}
	encoded, err := EncodeSOPCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSOPCursor(encoded)
	if err != nil || got.SOPID != want.SOPID || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("round trip: cursor=%+v err=%v", got, err)
	}
	for _, value := range []string{"", "not-a-cursor"} {
		if _, err := DecodeSOPCursor(value); err == nil {
			t.Fatalf("DecodeSOPCursor(%q) succeeded", value)
		}
	}
}

func TestTaskCursorRoundTripSupportsDatedAndUndatedTasks(t *testing.T) {
	dueAt := time.Date(2026, 7, 21, 9, 0, 0, 123, time.UTC)
	for _, want := range []TaskCursor{
		{DueAt: &dueAt, TaskID: "20000000-0000-4000-8000-000000000001"},
		{TaskID: "20000000-0000-4000-8000-000000000002"},
	} {
		encoded, err := EncodeTaskCursor(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeTaskCursor(encoded)
		if err != nil || got.TaskID != want.TaskID {
			t.Fatalf("round trip: cursor=%+v err=%v", got, err)
		}
		if (got.DueAt == nil) != (want.DueAt == nil) || (want.DueAt != nil && !got.DueAt.Equal(*want.DueAt)) {
			t.Fatalf("round trip due_at: got=%v want=%v", got.DueAt, want.DueAt)
		}
	}
	for _, value := range []string{"", "not-a-cursor"} {
		if _, err := DecodeTaskCursor(value); err == nil {
			t.Fatalf("DecodeTaskCursor(%q) succeeded", value)
		}
	}
}
