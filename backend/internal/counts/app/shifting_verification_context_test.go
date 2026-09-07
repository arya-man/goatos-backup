package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// A verifier judges a shifting clip of animals WALKING BETWEEN TWO PENS. Until this test, the
// enqueued item named only where they arrived, so half the claim on screen could not be checked
// against the video: the reviewer saw a pen she could not place and a label that stated the other
// one. These tests pin both halves on the production Complete path -- the composed queue label and
// the labelled rows the detail screen renders beside the video.

type recordingShiftingRepo struct {
	ports.Repository
	result domain.ShiftingExecutionResult
}

func (r *recordingShiftingRepo) CompleteShiftingEvent(
	context.Context, domain.ShiftingCompletionCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	return r.result, false, nil
}

type capturingEnqueuer struct {
	request ShiftingVerificationEnqueueRequest
}

func (c *capturingEnqueuer) EnqueueShiftingMoveVerification(
	_ context.Context, in ShiftingVerificationEnqueueRequest,
) error {
	c.request = in
	return nil
}

// completeAndCapture drives the real service Complete path and hands back what reached the queue.
func completeAndCapture(t *testing.T, result domain.ShiftingExecutionResult) ShiftingVerificationEnqueueRequest {
	t.Helper()
	enqueuer := &capturingEnqueuer{}
	svc := NewShiftingExecutionService(&recordingShiftingRepo{result: result}, func() time.Time {
		return time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	}).WithVerificationEnqueuer(enqueuer)

	if _, _, err := svc.Complete(context.Background(), CompleteShiftingInput{
		TenantID: "tenant-1", ShiftingEventID: "event-1", CompletedByUserID: "operator-1",
		ProofRef: "proof-1", IdempotencyKey: "key-1", RequestFingerprint: "fingerprint-1",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return enqueuer.request
}

func TestShiftingVerificationNamesBothPensTheAnimalsMovedBetween(t *testing.T) {
	got := completeAndCapture(t, domain.ShiftingExecutionResult{
		ShiftingEventID:           "event-1",
		EventStatus:               domain.ShiftingEventStatusPendingVerification,
		SourceShedName:            "Mandela 1",
		SourcePartitionLabel:      "Part 2",
		DestinationShedName:       "Castro",
		DestinationPartitionLabel: "3",
		MovedGoatIDs:              []string{"g1", "g2", "g3"},
	})

	// The queue LIST reads this one line, so both pens have to survive in it. The destination stays
	// the segment right after "Pen move": admin-web promotes the segment matching the item's own
	// resolved location to the headline, and Android drops its appended pen label when the subject
	// already carries it, so leading with the source would demote the destination on both.
	const wantSubject = "Pen move · Castro 3 · from Mandela 1 - Part 2 · 3 animals"
	if got.SubjectLabel != wantSubject {
		t.Errorf("subject label = %q, want %q", got.SubjectLabel, wantSubject)
	}

	// The DETAIL screen renders these verbatim beside the video, in this order -- the direction the
	// animals walked. Both surfaces already render backend context rows generically, which is why
	// neither client needed a change.
	want := []VerificationContextRow{
		{Label: "Moved from", Value: "Mandela 1 - Part 2"},
		{Label: "Moved to", Value: "Castro 3"},
	}
	if len(got.ContextRows) != len(want) {
		t.Fatalf("context rows = %#v, want %#v", got.ContextRows, want)
	}
	for i, row := range want {
		if got.ContextRows[i] != row {
			t.Errorf("context row %d = %#v, want %#v", i, got.ContextRows[i], row)
		}
	}

	// Composition, not raw storage: a bare "Castro" and a bare "Mandela 1" are DIFFERENT PLACES from
	// the partitioned pens above, and shipping either would send the verifier to the wrong pen.
	if strings.Contains(got.SubjectLabel, "Castro ·") || strings.Contains(got.SubjectLabel, "Mandela 1 ·") {
		t.Errorf("subject label dropped a partition: %q", got.SubjectLabel)
	}

	// The item's own partition, which shifting never set before this change: without it the queue's
	// operational location composes as the bare shed "Castro" instead of the pen "Castro 3".
	if got.PartitionLabel != "3" {
		t.Errorf("partition label = %q, want %q", got.PartitionLabel, "3")
	}
}

// A row recording no source (initial placement, and rows predating the source columns) states the
// destination ALONE. A dangling "from " or a "Moved from" row with an empty value reads as a bug to
// the verifier and would be worse than saying nothing.
func TestShiftingVerificationWithNoRecordedSourceStatesTheDestinationAlone(t *testing.T) {
	got := completeAndCapture(t, domain.ShiftingExecutionResult{
		ShiftingEventID:     "event-1",
		EventStatus:         domain.ShiftingEventStatusPendingVerification,
		DestinationShedName: "Yashoda 2",
		MovedGoatIDs:        []string{"g1"},
	})

	const wantSubject = "Pen move · Yashoda 2 · 1 animals"
	if got.SubjectLabel != wantSubject {
		t.Errorf("subject label = %q, want %q", got.SubjectLabel, wantSubject)
	}
	if strings.Contains(got.SubjectLabel, "from") {
		t.Errorf("subject label invented a source: %q", got.SubjectLabel)
	}
	want := []VerificationContextRow{{Label: "Moved to", Value: "Yashoda 2"}}
	if len(got.ContextRows) != 1 || got.ContextRows[0] != want[0] {
		t.Errorf("context rows = %#v, want %#v", got.ContextRows, want)
	}
}
