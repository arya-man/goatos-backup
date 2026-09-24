package readcache

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter          = otel.Meter("github.com/vgoats/goatos/backend/readcache")
	lookupCounter  = mustCounter("goatos.read_cache.lookups", "{lookup}", "Read-cache lookups by outcome: hit, stale (served while one background refresh runs), miss (ran the load), shared (joined an in-flight load), refresh.")
	evictedCounter = mustCounter("goatos.read_cache.evicted", "{entry}", "Read-cache entries evicted by a committed write (scoped) or an eviction-feed reconnect (all).")
)

func mustCounter(name, unit, desc string) metric.Int64Counter {
	c, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(fmt.Errorf("readcache: create counter %s: %w", name, err))
		return nil
	}
	return c
}

func record(ctx context.Context, cache, outcome string) {
	if lookupCounter == nil {
		return
	}
	lookupCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("cache", cache), attribute.String("outcome", outcome)))
}

func recordEvict(ctx context.Context, cache, kind string, n int) {
	if evictedCounter == nil || n == 0 {
		return
	}
	evictedCounter.Add(ctx, int64(n), metric.WithAttributes(attribute.String("cache", cache), attribute.String("kind", kind)))
}
