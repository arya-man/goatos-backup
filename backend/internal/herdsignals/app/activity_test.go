package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

func TestComputeMotionChangePercent(t *testing.T) {
	tests := []struct {
		name        string
		before      int64
		after       int64
		expectedPct int64
	}{
		// Test case: no before movement, movement after
		{
			name:        "no before, movement after",
			before:      0,
			after:       10,
			expectedPct: 100,
		},
		// Test case: no before, no after
		{
			name:        "no movement at all",
			before:      0,
			after:       0,
			expectedPct: 0,
		},
		// Test case: no before, negative after (shouldn't happen but handle it)
		{
			name:        "no before, negative after",
			before:      0,
			after:       -10,
			expectedPct: -100,
		},
		// Test case: normal increase
		{
			name:        "50% increase",
			before:      100,
			after:       150,
			expectedPct: 50,
		},
		// Test case: decrease
		{
			name:        "50% decrease",
			before:      100,
			after:       50,
			expectedPct: -50,
		},
		// Test case: double
		{
			name:        "100% increase (double)",
			before:      50,
			after:       100,
			expectedPct: 100,
		},
		// Test case: small change
		{
			name:        "10% increase",
			before:      100,
			after:       110,
			expectedPct: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeMotionChangePercent(tt.before, tt.after)
			if result != tt.expectedPct {
				t.Errorf("computeMotionChangePercent(%d, %d) = %d, want %d", tt.before, tt.after, result, tt.expectedPct)
			}
		})
	}
}

func TestSumMotionDeltas(t *testing.T) {
	tests := []struct {
		name               string
		windows            []domain.ActivityWindow
		expectedSum        *int64
		expectedIncomplete bool
	}{
		// Test case: empty windows
		{
			name:        "empty windows",
			windows:     []domain.ActivityWindow{},
			expectedSum: nil,
		},
		// Test case: normal buckets
		{
			name: "normal buckets",
			windows: []domain.ActivityWindow{
				{MotionDelta: 10, IsGap: false, GapDelta: false},
				{MotionDelta: 20, IsGap: false, GapDelta: false},
				{MotionDelta: 15, IsGap: false, GapDelta: false},
			},
			expectedSum:        ptrInt64(45),
			expectedIncomplete: false,
		},
		// Test case: gap in middle
		{
			name: "gap in middle",
			windows: []domain.ActivityWindow{
				{MotionDelta: 10, IsGap: false, GapDelta: false},
				{MotionDelta: 0, IsGap: true, GapDelta: false}, // Gap bucket
				{MotionDelta: 20, IsGap: false, GapDelta: false},
			},
			expectedSum:        ptrInt64(30),
			expectedIncomplete: true,
		},
		// Test case: reconnect delta
		{
			name: "reconnect delta",
			windows: []domain.ActivityWindow{
				{MotionDelta: 10, IsGap: false, GapDelta: false},
				{MotionDelta: 100, IsGap: false, GapDelta: true}, // Reconnect bucket with unknown distribution
				{MotionDelta: 20, IsGap: false, GapDelta: false},
			},
			expectedSum:        ptrInt64(130),
			expectedIncomplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sum, incomplete := sumMotionDeltas(tt.windows)
			if (sum == nil) != (tt.expectedSum == nil) {
				t.Errorf("sumMotionDeltas: expected sum nil=%v, got nil=%v", tt.expectedSum == nil, sum == nil)
			}
			if sum != nil && tt.expectedSum != nil && *sum != *tt.expectedSum {
				t.Errorf("sumMotionDeltas: expected sum %d, got %d", *tt.expectedSum, *sum)
			}
			if incomplete != tt.expectedIncomplete {
				t.Errorf("sumMotionDeltas: expected incomplete=%v, got %v", tt.expectedIncomplete, incomplete)
			}
		})
	}
}

func ptrInt64(i int64) *int64 {
	return &i
}

// A bucket with no packets has NO ROW in herd_signal_activity_windows -- it is not a row with
// packet_count = 0. So an outage inside a correlation half shows up as MISSING BUCKETS, and summing
// only the rows that came back yields a clean total and a confident percentage across a hole.
//
// This is the case the earlier IsGap check could never catch, because IsGap can only be set on a row
// that exists. The test asserts the arithmetic of that detection: a half holding fewer buckets than
// the window should contain is incomplete, whatever the rows it does hold say.
func TestSparseHalfIsIncompleteEvenWhenEveryReturnedRowLooksHealthy(t *testing.T) {
	const bucketSeconds = correlationBucketSeconds
	expectedPerHalf := int(time.Duration(correlationWindowHours) * time.Hour / (time.Duration(bucketSeconds) * time.Second))
	if expectedPerHalf != 24 {
		t.Fatalf("expected 24 buckets per 2h half at %ds, got %d", bucketSeconds, expectedPerHalf)
	}

	healthy := func(n int) []domain.ActivityWindow {
		out := make([]domain.ActivityWindow, 0, n)
		base := time.Date(2026, 8, 23, 6, 0, 0, 0, time.UTC)
		for i := 0; i < n; i++ {
			out = append(out, domain.ActivityWindow{
				BucketStart:   base.Add(time.Duration(i*bucketSeconds) * time.Second),
				BucketSeconds: bucketSeconds,
				MotionDelta:   10,
				PacketCount:   5, // packets arrived: nothing about THESE rows looks wrong
			})
		}
		return out
	}

	full := healthy(expectedPerHalf)
	if _, incomplete := sumMotionDeltas(full); incomplete {
		t.Fatalf("a fully covered half must not be incomplete")
	}
	if sparse := len(full) < expectedPerHalf; sparse {
		t.Fatalf("a fully covered half must not be sparse")
	}

	// One hour of the two is simply absent -- no rows at all, the ordinary shape of an outage.
	half := healthy(expectedPerHalf / 2)
	if _, incomplete := sumMotionDeltas(half); incomplete {
		t.Fatalf("the returned rows are all healthy, so row-level inspection alone cannot detect the hole -- which is the point")
	}
	if sparse := len(half) < expectedPerHalf; !sparse {
		t.Fatalf("a half holding %d of %d buckets must be treated as sparse, and therefore incomplete", len(half), expectedPerHalf)
	}
}

// The batch is fetched once across EVERY event's span, so it holds buckets that belong to other
// events entirely. Each event must be clipped to its own two hours before anything is summed.
//
// Without the clip the first event counted every later bucket as its "after" and the last event
// counted every earlier bucket as its "before" -- so a response to feeding could be inflated by a
// weighing hours later. Worse, those extra buckets padded the count that the sparse check compares
// against expectedPerHalf, so a genuine hole INSIDE the real window could be hidden by unrelated
// buckets outside it: the one thing that comparison exists to refuse.
func TestBatchedCorrelationClipsEachEventToItsOwnWindow(t *testing.T) {
	const bucketSeconds = correlationBucketSeconds
	expectedPerHalf := int(time.Duration(correlationWindowHours) * time.Hour / (time.Duration(bucketSeconds) * time.Second))

	base := time.Date(2026, 8, 23, 6, 0, 0, 0, time.UTC)
	// Two events eight hours apart: their windows do not overlap at all.
	eventA := base
	eventB := base.Add(8 * time.Hour)

	// One continuous run of buckets spanning both events and the gap between them, every bucket
	// healthy. A correct implementation gives each event exactly its own 24-bucket halves.
	var all []domain.ActivityWindow
	for t0 := eventA.Add(-2 * time.Hour); t0.Before(eventB.Add(2 * time.Hour)); t0 = t0.Add(time.Duration(bucketSeconds) * time.Second) {
		all = append(all, domain.ActivityWindow{
			BucketStart:   t0,
			BucketSeconds: bucketSeconds,
			MotionDelta:   1,
			PacketCount:   3,
		})
	}

	svc := &Service{}
	for _, tc := range []struct {
		name string
		at   time.Time
	}{{"first event", eventA}, {"last event", eventB}} {
		t.Run(tc.name, func(t *testing.T) {
			ev := domain.ActivityEvent{At: tc.at}
			svc.computeEventCorrelationFromBatch(&ev, all, expectedPerHalf)

			if ev.MotionDeltaBefore2h == nil || ev.MotionDeltaAfter2h == nil {
				t.Fatalf("both halves are fully covered, so both must produce a number; got before=%v after=%v",
					ev.MotionDeltaBefore2h, ev.MotionDeltaAfter2h)
			}
			// Every bucket carries delta 1, so a correctly clipped half sums to exactly its bucket count.
			if *ev.MotionDeltaBefore2h != int64(expectedPerHalf) {
				t.Fatalf("before half must contain exactly %d buckets from THIS event's window, summed %d -- buckets outside the window leaked in",
					expectedPerHalf, *ev.MotionDeltaBefore2h)
			}
			if *ev.MotionDeltaAfter2h != int64(expectedPerHalf) {
				t.Fatalf("after half must contain exactly %d buckets from THIS event's window, summed %d -- buckets outside the window leaked in",
					expectedPerHalf, *ev.MotionDeltaAfter2h)
			}
			if ev.BeforeWindowIncomplete || ev.AfterWindowIncomplete {
				t.Fatalf("a fully covered window must not be reported incomplete")
			}
		})
	}

	// Now punch a hole INSIDE the first event's before-half, while leaving the wider batch dense.
	// Padding from outside the window must not disguise it.
	holeStart := eventA.Add(-1 * time.Hour)
	var withHole []domain.ActivityWindow
	for _, w := range all {
		if w.BucketStart.Equal(holeStart) {
			continue
		}
		withHole = append(withHole, w)
	}
	ev := domain.ActivityEvent{At: eventA}
	svc.computeEventCorrelationFromBatch(&ev, withHole, expectedPerHalf)
	if !ev.BeforeWindowIncomplete {
		t.Fatal("a bucket missing from INSIDE the before-window must mark it incomplete, even though the wider batch is dense")
	}
	if ev.MotionChangePercent != nil {
		t.Fatal("an incomplete half must not yield a percentage change")
	}
}

// Farm events do not arrive on 5-minute boundaries. A vaccination is recorded at 06:02:17, not
// 06:00:00 — so if the correlation only produces a whole number of buckets for aligned timestamps,
// it fails for essentially every real event.
//
// Clipping to a raw event.At +/- 2h gave 23 wholly-contained buckets before an unaligned event, not
// 24: the bucket at the window's far edge hangs over the boundary and the bucket containing the
// event straddles it. The count check then marked healthy activity incomplete and blanked its
// before/after/change. Every earlier test used a round timestamp, so all of them passed.
func TestBatchedCorrelationHandlesUnalignedEventTimes(t *testing.T) {
	const bucketSeconds = correlationBucketSeconds
	expectedPerHalf := int(time.Duration(correlationWindowHours) * time.Hour / (time.Duration(bucketSeconds) * time.Second))

	for _, at := range []time.Time{
		time.Date(2026, 8, 23, 6, 2, 17, 0, time.UTC), // mid-bucket, the ordinary case
		time.Date(2026, 8, 23, 6, 4, 59, 0, time.UTC), // one second before a boundary
		time.Date(2026, 8, 23, 6, 0, 0, 0, time.UTC),  // exactly on a boundary
		time.Date(2026, 8, 23, 6, 0, 1, 0, time.UTC),  // one second after a boundary
	} {
		t.Run(at.Format("15:04:05"), func(t *testing.T) {
			// Dense buckets either side, well beyond the two hours each half needs.
			var all []domain.ActivityWindow
			gridStart := at.Truncate(time.Duration(bucketSeconds) * time.Second).Add(-3 * time.Hour)
			for t0 := gridStart; t0.Before(at.Add(3 * time.Hour)); t0 = t0.Add(time.Duration(bucketSeconds) * time.Second) {
				all = append(all, domain.ActivityWindow{
					BucketStart:   t0,
					BucketSeconds: bucketSeconds,
					MotionDelta:   1,
					PacketCount:   4,
				})
			}

			ev := domain.ActivityEvent{At: at}
			(&Service{}).computeEventCorrelationFromBatch(&ev, all, expectedPerHalf)

			if ev.BeforeWindowIncomplete || ev.AfterWindowIncomplete {
				t.Fatalf("coverage is dense on both sides, so an event at %s must not be reported incomplete", at.Format(time.RFC3339))
			}
			if ev.MotionDeltaBefore2h == nil || *ev.MotionDeltaBefore2h != int64(expectedPerHalf) {
				t.Fatalf("before half must total exactly %d whole buckets for an event at %s; got %v",
					expectedPerHalf, at.Format(time.RFC3339), ev.MotionDeltaBefore2h)
			}
			if ev.MotionDeltaAfter2h == nil || *ev.MotionDeltaAfter2h != int64(expectedPerHalf) {
				t.Fatalf("after half must total exactly %d whole buckets for an event at %s; got %v",
					expectedPerHalf, at.Format(time.RFC3339), ev.MotionDeltaAfter2h)
			}
			if ev.MotionChangePercent == nil {
				t.Fatalf("both halves are complete and equal, so a change of 0%% must be reported for an event at %s", at.Format(time.RFC3339))
			}
		})
	}
}

// fetchBoundsRecorder answers ListActivityWindows the way the real repository does -- returning only
// buckets whose bucket_start falls inside [from, to) -- and records the bounds it was asked for.
//
// This exists because testing computeEventCorrelationFromBatch alone could never catch a bad FETCH:
// that helper is handed its buckets, so a test building them from the grid hands it exactly what it
// wants regardless of what the service would really have requested.
type fetchBoundsRecorder struct {
	ports.Repository // nil: only ListActivityWindows is exercised here
	grid             []domain.ActivityWindow
	gotFrom, gotTo   time.Time
}

func (f *fetchBoundsRecorder) ListActivityWindows(_ context.Context, _, _ string, from, to time.Time, _ int) ([]domain.ActivityWindow, error) {
	f.gotFrom, f.gotTo = from, to
	var out []domain.ActivityWindow
	for _, w := range f.grid {
		// The real query selects on bucket_start, so a bucket starting before `from` is invisible
		// even when part of it lies inside the requested range.
		if !w.BucketStart.Before(from) && w.BucketStart.Before(to) {
			out = append(out, w)
		}
	}
	return out, nil
}

// An unaligned event's before-half begins at the start of the bucket two hours before its OWN
// bucket. Fetching from a raw event-2h misses that first bucket entirely, so the helper counts one
// short and blanks a perfectly healthy correlation. The bug lived in the fetch bounds, one layer
// below every earlier test.
func TestBatchedCorrelationFetchesTheWholeAnchoredWindow(t *testing.T) {
	const bucketSeconds = correlationBucketSeconds
	bucketDur := time.Duration(bucketSeconds) * time.Second
	expectedPerHalf := int(time.Duration(correlationWindowHours) * time.Hour / bucketDur)

	at := time.Date(2026, 8, 23, 6, 2, 17, 0, time.UTC) // deliberately mid-bucket
	var grid []domain.ActivityWindow
	for t0 := at.Truncate(bucketDur).Add(-4 * time.Hour); t0.Before(at.Add(4 * time.Hour)); t0 = t0.Add(bucketDur) {
		grid = append(grid, domain.ActivityWindow{
			BucketStart: t0, BucketSeconds: bucketSeconds, MotionDelta: 1, PacketCount: 4,
		})
	}

	repo := &fetchBoundsRecorder{grid: grid}
	svc := &Service{repo: repo, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	events := []domain.ActivityEvent{{At: at}}
	svc.computeEventCorrelationsBatched(context.Background(), "tenant", "tag", events)

	// The earliest bucket the maths needs is the one starting two hours before the event's bucket.
	needFrom := at.Truncate(bucketDur).Add(time.Duration(-correlationWindowHours) * time.Hour)
	if repo.gotFrom.After(needFrom) {
		t.Fatalf("fetch started at %s but the anchored before-half needs the bucket at %s; the first bucket can never be returned",
			repo.gotFrom.Format(time.RFC3339), needFrom.Format(time.RFC3339))
	}

	ev := events[0]
	if ev.BeforeWindowIncomplete || ev.AfterWindowIncomplete {
		t.Fatalf("coverage is dense either side of %s, so neither half may be reported incomplete", at.Format(time.RFC3339))
	}
	if ev.MotionDeltaBefore2h == nil || *ev.MotionDeltaBefore2h != int64(expectedPerHalf) {
		t.Fatalf("before half must total %d whole buckets; got %v", expectedPerHalf, ev.MotionDeltaBefore2h)
	}
	if ev.MotionDeltaAfter2h == nil || *ev.MotionDeltaAfter2h != int64(expectedPerHalf) {
		t.Fatalf("after half must total %d whole buckets; got %v", expectedPerHalf, ev.MotionDeltaAfter2h)
	}
}
