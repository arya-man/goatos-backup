# scale-guard

Zero-dependency static gate that blocks the million-animal scale anti-patterns
in [`../../docs/decisions/scale-anti-patterns.md`](../../docs/decisions/scale-anti-patterns.md)
from re-entering `main`.

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
| `offset-pagination` | `OFFSET <bind>` in a SQL literal |
| `full-mv-refresh` | `DELETE FROM <...projection...> WHERE ... tenant_id` |
| `non-sargable-like` | `lower(col) LIKE '%..%'` |
| `god-cte` | > 8 `x AS (` CTEs in one request-path SQL literal |

One-time tooling (`backend/cmd/seed-*`, `migrate`) is out of scope.

## Escape hatches

- **Inline** (bounded case): append `// scale-guard:ignore: <reason>` to the line
  (or the line above). Reason mandatory.
- **Baseline** (pre-existing debt): add `<rule> <relpath>:<line>` to
  [`baseline.txt`](baseline.txt). It is a burn-down list — delete a line when you
  fix the code; the guard warns on stale entries.

## Limits

Checks query **shape**, not runtime cost. It does not replace the EXPLAIN plan /
latency gates (which today run at ~1k rows). It also does not catch the
non-terminating pagination loop (a control-flow bug) — that stays a review item.
