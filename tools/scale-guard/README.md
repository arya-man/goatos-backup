# scale-guard

Zero-dependency static gate that blocks the scale anti-patterns
in [`../../docs/decisions/scale-anti-patterns.md`](../../docs/decisions/scale-anti-patterns.md)
from re-entering `main`. The patterns are fast at ~1k rows and fatal at high
scale, so the guard runs regardless of release target. Per the
[Operational kernel 5k-to-50k scale envelope ADR](../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md),
the current release envelope is 5,000-50,000 animals (query-plan proof at the
~500k obligation-row upper bound); 1-5M-animal deployment is the future
certification bar, not a present release requirement.

## Run

```bash
make scale-guard                      # from repo root (wired into `make check` + CI)
cd tools/scale-guard && go run . -root ../..
```

Exit 1 on any NEW violation. Green when every offender is baselined or ignored.

## What it flags (in `backend/internal/**`)

| rule | trigger |
|---|---|
| `n-plus-one` | `.Query/.QueryRow/.Exec/.SendBatch` inside a `for`/`range` |
| `n-plus-one-fanout` | a ctx-taking call to an injected I/O dependency (repo/reader/port/client/roster/ownership) inside a `for`/`range` — the raw driver call sits one adapter layer down, invisible to `n-plus-one`. One round trip per row (a 25-row page → 51 serial reads). Batch to one `*ByIDs` / `= ANY($1)` read |
| `loop-no-cursor` | infinite `for {}` paging loop with no cursor/progress guard |
| `offset-pagination` | `OFFSET <bind>` in a SQL literal |
| `full-mv-refresh` | whole-tenant projection delete with no `projection_version` guard |
| `non-sargable-like` | `lower(col) LIKE '%..%'` |
| `non-sargable-cast` | indexed column cast to text in an `ANY` or `IN (...)` predicate (`id::text = ANY(...)`, `id::text IN (...)`, ebe349c37); cast the typed bind array instead |
| `god-cte` | > 8 `x AS (` CTEs in one request-path SQL literal |
| `cte-limit-outside` | a paginated statement (top-level `ORDER BY` + `LIMIT`) whose scanning CTE has no `LIMIT` of its own — every request materialises the whole underlying set and then keeps a page. Work proportional to the table, not the page. Unlike the other SQL rules this one reads the **fully assembled** statement (package-level consts resolved through their `+` chains), because the CTE and the LIMIT routinely sit in different fragments |
| `count-distinct-sort` | `COUNT(DISTINCT x)` in SQL: Postgres sorts every input row for a DISTINCT aggregate. Collapse to one row per key (GROUP BY / SELECT DISTINCT, hashable) and count those (cb0c2d0dc 725→276 ms, ba2984573 805→232 ms). Pre-existing uses are ratcheted in `baseline.txt` |
| `cte-self-join` | a CTE joined directly to itself (`FROM c a JOIN c b`) in an assembled adapter statement; a generic plan turns it into a nested loop of two CTE scans (ca7b21a82, 800→100 ms). Pair with a window (LAG/LEAD/MIN() OVER). Adjacent shape only; see blind spots in `perfpatterns.go` |
| `hand-rolled-read-cache` | a struct named `*cache*` holding a map + `sync.Mutex`/`RWMutex` outside `backend/internal/platform/readcache` — use the shared SWR cache with scoped eviction (95b1054c1, a056df98a) |
| `or-subquery-membership` | an OR (any depth, WHERE/ON) with a branch `IN (SELECT…)` / `EXISTS (…)` / `= ANY(SELECT…)` / `= ANY(ARRAY(SELECT…))` filtering a large table (`largeTables` in `orsubquery.go`) at its own query level: at 500k rows the planner cannot BitmapOr the arms and seq-scans (PP-22, processintegrity 1.2 s). Split into UNION ALL per arm; an ignore must name a `Test*AtScale` / `validate-sqlc-plans` plan test. Bind-only arms (`NOT $5::bool OR …`) and subqueries over `unnest`/`VALUES` are not flagged |
| `read-rollup-truth` | request-path/service rollup that bumps a raw list limit or clears `NextCursor` after in-memory aggregation |

One-time tooling (`backend/cmd/seed-*`, `migrate`) is out of scope.

## Plan-proof mode (`make scale-guard-plan-proof`)

`go run . -root <repo> -plan-proof [-base origin/main]` diffs against the merge-base and fails when a
changed serving statement (backend/internal/**/adapters, SELECT over a large table) has no
changed/added `Test*AtScale` test in the same package (or one naming the statement), and no added
`explain_*` entry in `validate-sqlc-query-plans.sh` for sqlc. Exempt a plan-neutral change with
`scale-guard:plan-proof-exempt: <reason>` on a changed line. `-list <rule>` prints every finding of a rule.

## Escape hatches

- **Inline** (bounded case): append `// scale-guard:ignore: <reason>` to the line
  (or the line above). Reason mandatory.
- **Baseline** (pre-existing debt): add `<rule> <relpath> <count>` to
  [`baseline.txt`](baseline.txt). It is a burn-down list — reduce a count when
  you fix code. Counts are per `(rule, file)`, so harmless line movement does
  not break CI.

## Limits

Checks query **shape**, not runtime cost. It does not replace the EXPLAIN plan /
latency gates (which today run at ~1k rows). The `loop-no-cursor` rule is a
static heuristic for the park-consolidation hang class; it may need an inline
boundedness reason for intentionally finite loops.

`read-rollup-truth` exists because the broad "compute-on-write, never
compute-on-read" rule was still too easy to bypass with an app/service helper:
fetch a larger raw page, group it in memory, wipe pagination, and present the
collapsed card as truth. That shape is banned for Calendar, Action Center, and
other operator projections; the grouped row must come from a projector/read
model or an explicitly partial/debug-only path.
