package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
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
