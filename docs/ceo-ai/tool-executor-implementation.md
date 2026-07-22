# CEO AI Tool Executor Implementation Guide

## Overview

The leadership assistant's Tier-2 (API) routing layer uses in-process read service tool executors
to fetch operational data directly from the Mesha backend. This document describes the implementation
architecture and wiring of these executors, and how the Cube -> API -> Toolbox -> SQL runtime
fallback keeps a question answerable even when a given tier's executor is unwired or fails.

## Executors

The following tool executors are registered in `backend/internal/ceoai/adapters/readtools/toolexecutors.go`
and (optionally) wired with a real data reader in `backend/internal/bootstrap/api.go`. Every
executor's `Spec().Name` MUST exactly match the tool name the planner (both
`adapters/vertex` and `adapters/keywordplanner`) routes that intent to — `registry.go`'s
`RouteAPI` case looks executors up strictly by name (`r.executors[sub.ToolName]`), so a mismatch
here dead-ends the whole question on "no read-service executor" (this is what happened to feed
before the planner's tool name was corrected to `feed_direction_today`).

### 1. countsBreakdownExecutor
- **Tool Name**: `counts_breakdown`
- **Route**: RouteAPI
- **Description**: Animal counts broken down by park, shed, breed, sex, stage, or species
- **Data Source**: not wired with an in-process reader today; when unwired the executor returns
  `ToolResult.Err = "counts data reader not wired"` (never a silently swallowed empty result)
- **Coverage**: GET /counts/breakdown, GET /herd-register/summary
- **Fallback**: `active_animals` (Cube, species/park/shed grouping) then `mesha_count_by_scope`
  (MCP Toolbox tool over `ceo_ai.animal_current_scope`, supports breed/sex/stage grouping too)

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
- **Data Source**: not wired with an in-process reader today; when unwired the executor returns
  `ToolResult.Err = "feed data reader not wired"`
- **Coverage**: GET /feed-direction/preview
- **Fallback**: no governed Cube feed metric exists yet, so the fallback is the MCP Toolbox
  `mesha_feed_direction_summary` tool over `ceo_ai.feed_direction_current`

## Wiring Pattern

Executors are initialized with optional data reader functions, of the shape
`func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error)`.
`params` is the sub-question's full param set — `dimension`/`park_label`/`species`/`shed_id` plus
`as_of` (the resolved IST business date the orchestrator injects on every sub-question) — so a
wired reader can honor scope and as-of, not just tenant. These closures are injected at bootstrap
time to provide real backend service calls:

```go
// Bootstrap wiring (backend/internal/bootstrap/api.go)
readToolExecs := ceoreadtools.NewToolExecutors()

// Wire counts data reader
countsDataReader := func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
    dimension, _ := params["dimension"].(string)
    parkLabel, _ := params["park_label"].(string)
    // ... query ceo_ai.animal_current_scope grouped by dimension, scoped by parkLabel/species/etc.
    return facts, nil
}
ceoreadtools.SetCountsDataReader(readToolExecs[0], countsDataReader)
```

## Runtime Fallback (Cube -> API -> Toolbox -> SQL)

`registry.go` only ever executes the ONE tier a sub-question was routed to. Previously an
errored/unwired API executor (or a review pass that marked the composed answer incomplete) went
straight to the user as "isn't available yet" even when another tier could answer the same
question — this was the P1-3 gap. The orchestrator (`app/fallback.go`) now:

1. Runs the planner's chosen route first, same as before.
2. If the result errored (`ToolResult.Err`, or a Go error from an unwired port) OR came back with
   zero grounding `Facts`, looks up a `fallbackAlias` for that tool name and retries the SAME
   sub-question at the next tier in Cube -> API -> Toolbox -> SQL order (skipping the tier that
   already ran), stopping at the first tier that returns real facts.
3. After the runtime review pass (`review.go`), also honors `verdict.Complete` (previously
   ignored): an answer that is grounded/scope-safe but incomplete gets one more fallback-tier
   retry on its still-failed/empty results before the pipeline downgrades to the honest
   "couldn't fully verify" message. Only after every aliased tier is exhausted does the answer
   degrade — never before.

`fallbackAliases` in `app/fallback.go` is the source of truth for which tools are equivalent
across tiers (currently `counts_breakdown` and `feed_direction_today`); extend it whenever a new
API tool gains a Cube metric or Toolbox tool that answers the same question.

## Tool-step tracing

`stepExecutor.run` (`app/loop.go`) records both failure shapes into the internal `StepTrace.Err`
used by the admin trace/audit: a real Go error from the executor call, AND a nil-error /
non-nil-`ToolResult.Err` step (the shape every readtools executor uses for "reader not wired").
Before this was fixed (P2-5), only the Go error was recorded, so a failed-but-not-crashed tool
step (the common case) was invisible in `GET /ceo-ai/admin/trace/{request_id}`.

## Extending Executors

To wire an additional data reader (vaccination, feed, etc.):

1. Implement a closure of shape `func(ctx, tenantID string, params map[string]any) ([]domain.Fact, error)`
   that calls the real read service, honoring the scope/as-of params it needs.
2. Transform the service response into a `[]domain.Fact` array.
3. Call the appropriate `SetXxxDataReader` function at bootstrap time.
4. If the tool now has a real reader AND an equivalent Cube metric or Toolbox tool exists, add/
   update its entry in `fallbackAliases` (`app/fallback.go`) so a failure still falls through.
5. Add test coverage for the data transformation and for params/as-of threading (see
   `toolexecutors_test.go`'s `TestCountsBreakdownExecutor_ThreadsScopeParams` for the pattern).

See the `counts_breakdown` implementation as a reference pattern.
