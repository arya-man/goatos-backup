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
| `loop-no-cursor` | infinite `for {}` paging loop with no cursor/progress guard |
| `offset-pagination` | `OFFSET <bind>` in a SQL literal |
| `full-mv-refresh` | whole-tenant projection delete with no `projection_version` guard |
| `non-sargable-like` | `lower(col) LIKE '%..%'` |
| `god-cte` | > 8 `x AS (` CTEs in one request-path SQL literal |

One-time tooling (`backend/cmd/seed-*`, `migrate`) is out of scope.

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
