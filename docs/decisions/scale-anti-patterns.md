# Scale Anti-Patterns

Status: active guardrail.

Goat OS treats million-animal scale as a release invariant. New request paths,
workers, importers, projectors, dashboards, and reporting paths must be tenant
scoped, indexed, bounded, resumable, and measurable.

`make scale-guard` blocks new static offenders for the highest-risk patterns:

- compute-on-read god CTEs on request paths
- N+1 database calls inside loops
- infinite paging loops without cursor/progress proof
- deep `OFFSET` pagination where keyset pagination is required
- tenant-wide projection delete/reinsert rebuilds
- non-sargable `lower(col) LIKE '%...'` search predicates

Existing debt is tracked in `tools/scale-guard/baseline.txt`. Do not add a new
baseline count for new work. Fix the query, batch the writes, add a real
projection/read model, use keyset cursors, prove loop progress, or add a narrow
inline `scale-guard:ignore` with a concrete boundedness reason.

When an E2E run, scale audit, or feature proof is generated while fixing one of
these issues, commit the report and publish it through the GitHub Pages report
site. Local-only proof must say that it is local-only and must not be described
as staging or production certification.
