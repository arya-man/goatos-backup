# CEO AI Tool Executor Implementation Guide

## Overview

The leadership assistant's Tier-2 (API) routing layer uses in-process read service tool executors
to fetch operational data directly from the Mesha backend. This document describes the implementation
architecture and wiring of these executors.

## Executors

The following tool executors are registered in `backend/internal/ceoai/adapters/readtools/toolexecutors.go`
and wired in `backend/internal/bootstrap/api.go`:

### 1. countsBreakdownExecutor
- **Tool Name**: `counts_breakdown`
- **Route**: RouteAPI
- **Description**: Animal counts broken down by species
- **Data Source**: `countsService.ProjectedCountFor(ctx, CountProjectionRequest)`
- **Coverage**: GET /counts/breakdown, GET /herd-register/summary
- **Implementation**: Queries counts projection rows and aggregates by species

### 2. vaccinationShedSummaryExecutor
- **Tool Name**: `vaccination_shed_summary`
- **Route**: RouteAPI
- **Description**: Vaccination status summary by shed
- **Data Source**: vaccination read services (deferred implementation)
- **Coverage**: GET /vaccination/execution/sheds/{shed_id}, GET /vaccination/sheds/{shed_id}

### 3. vaccinationExecutionExecutor
- **Tool Name**: `vaccination_execution`
- **Route**: RouteAPI
- **Description**: Vaccination execution status and drive details
- **Data Source**: vaccination execution read services (deferred implementation)
- **Coverage**: GET /vaccination/execution, GET /vaccination/schedule

### 4. feedDirectionTodayExecutor
- **Tool Name**: `feed_direction_today`
- **Route**: RouteAPI
- **Description**: Feed direction for today by shed
- **Data Source**: feed direction read services (deferred implementation)
- **Coverage**: GET /feed-direction/preview

## Wiring Pattern

Executors are initialized with optional data reader functions. These closures are injected at
bootstrap time to provide real backend service calls:

```go
// Bootstrap wiring (backend/internal/bootstrap/api.go)
readToolExecs := ceoreadtools.NewToolExecutors()

// Wire counts data reader
countsDataReader := func(ctx context.Context, tenantID string) ([]ceodomain.Fact, error) {
    counts, err := countsService.ProjectedCountFor(ctx, countsdomain.CountProjectionRequest{
        TenantID: tenantID,
        AsOf:     time.Now(),
    })
    // Transform counts projection rows into Facts
    // ...
    return facts, nil
}
ceoreadtools.SetCountsDataReader(readToolExecs[0], countsDataReader)
```

## Graceful Degradation

Executors handle missing or failed data readers gracefully:
- **No data reader**: Return empty Facts array (tool exists but no data)
- **Data reader error**: Return empty Facts array (orchestrator can try other routes)
- **No facts returned**: Return empty Facts array

This ensures the Cube-first routing hierarchy can fall back to alternative routes
(RouteCube, RouteToolbox, RouteSQL) when API tier tools return empty results.

## Extending Executors

To wire additional data readers (vacation, feed, etc.):

1. Implement a closure that calls the real read service
2. Transform the service response into `[]domain.Fact` array
3. Call the appropriate `SetXxxDataReader` function at bootstrap time
4. Add test coverage for the data transformation

See the counts_breakdown implementation as a reference pattern.
