package app

import (
	"context"
	"time"
)

type timingKey struct{}

// TimingRecorder receives gated, local diagnostic timings from the HTTP adapter.
// Production requests do not carry it, so the service keeps its normal hot path.
type TimingRecorder func(name string, elapsed time.Duration)

// WithTiming attaches a per-request diagnostic recorder to service calls.
func WithTiming(ctx context.Context, recorder TimingRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, recorder)
}

func recordTiming(ctx context.Context, name string, start time.Time) {
	recorder, _ := ctx.Value(timingKey{}).(TimingRecorder)
	if recorder == nil {
		return
	}
	recorder(name, time.Since(start))
}
