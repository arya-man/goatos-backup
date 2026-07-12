# Scale Anti-Patterns

Status: active guardrail.

Goat OS treats million-animal scale as a release invariant. New request paths,
workers, importers, projectors, dashboards, and reporting paths must be tenant
scoped, indexed, bounded, resumable, and measurable.

`make scale-guard` blocks new static offenders for the highest-risk patterns:

- compute-on-read god CTEs on request paths
- N+1 database calls inside loops (raw driver calls)
- N+1 fan-out: a ctx-taking call to an injected I/O dependency
  (repo/reader/port/client/roster/ownership) inside a loop — the driver call is
  one adapter layer down, invisible to the raw-driver N+1 check. The "small data,
  still slow" class (one round trip per row): a 25-row page becomes 51 serial
  reads. Fix by batching to a single `*ByIDs` / `= ANY($1)` read, as `ShedSummary`
  now does with `ShedOwnerships`.
- infinite paging loops without cursor/progress proof
- deep `OFFSET` pagination where keyset pagination is required
- tenant-wide projection delete/reinsert rebuilds
- non-sargable `lower(col) LIKE '%...'` search predicates
- capped read-time rollups that fetch a larger raw slice, aggregate it in
  app/service/frontend state, then hide pagination/truncation and present the
  collapsed result as business truth

Existing debt is tracked in `tools/scale-guard/baseline.txt`. Do not add a new
baseline count for new work. Fix the query, batch the writes, add a real
projection/read model, use keyset cursors, prove loop progress, or add a narrow
inline `scale-guard:ignore` with a concrete boundedness reason.

Calendar/Action Center/operator worklists need an extra explicit warning here:
if the product wants one park-drive row instead of hundreds of goat/protocol
rows, that grouped row must come from a projector/read model. It is not
acceptable to:

- bump the raw request-path limit (for example to 5000),
- aggregate those raw rows in a service/helper or frontend component,
- clear `NextCursor` or otherwise hide truncation,
- and then show shed/vaccine/goat counts as if they were complete business truth.

That pattern is still compute-on-read, still partial when the raw slice is
truncated, and still unsafe at 1M animals even if it looks fine on a local
fixture. If a temporary UI collapse is needed for a mock/demo, it must be
clearly partial/debug-only and must not invent authoritative totals.

The narrow Calendar exception is accepted vaccination completion history: a
read-only timeline behind the explicit `status=completed` path. That history is
not active work, not a park-drive rollup, and not a second projection row. It
may be derived at read time from canonical accepted completions plus completed
obligations when the query stays tenant-scoped, date-bounded, keyset-paginated,
and honest about truncation. Default month/date-marker responses may include
read-only completed markers so users can see recent completed days in the same
calendar surface, but open-work pages must not turn those markers into
aggregated operational totals, suppress pagination truth, or blur them into the
authoritative active-work list.

When an E2E run, scale audit, or feature proof is generated while fixing one of
these issues, commit the report and publish it through the GitHub Pages report
site. Local-only proof must say that it is local-only and must not be described
as staging or production certification.
