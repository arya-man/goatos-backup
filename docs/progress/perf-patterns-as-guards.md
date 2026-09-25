# Perf patterns as guards — enforcement ledger (2026-09-25)

Source: main perf commits since 2026-08-15 + PR #415 and its reviews.
Canonical prose: `docs/decisions/scale-anti-patterns.md` → "Proven performance patterns".

| Pattern | Enforcement | Baseline on main |
|---|---|---|
| PP-1 collapse-then-count | `scale-guard` `count-distinct-sort` | 36 (frozen) |
| PP-2 window pairing, no CTE self-join | `scale-guard` `cte-self-join` | 0 |
| PP-3 typed bind array, no column cast | `scale-guard` `non-sargable-cast` (+`::text IN`) | 0 new |
| PP-4 shared readcache | `scale-guard` `hand-rolled-read-cache` (eviction-on-write half review-only) | 3 |
| PP-5 lazy heavy SDKs | `admin-web-heavy-client-imports-guard` | 7 modules |
| PP-6 keyset / LIMIT in CTE | existing `offset-pagination`, `cte-limit-outside` | existing |
| PP-7..PP-21 | review-only (plan/runtime dependent) | — |
| PP-22 OR + subquery membership; at-scale plan proof | `scale-guard` rule `or-subquery-membership` + `scale-guard-plan-proof` | 19 statements / 11 files (origin/main counts; processintegrity not baselined) |

Checks run: `go test ./...` in tools/scale-guard (new pass/fail fixtures),
`make scale-guard` on main (ratchet pass), heavy-imports `--self-test` and real
check (pass), `make guardrail-registration-guard` (pass), `bash -n
tools/ci/run-local-ci.sh`, `node --check`. `make land-main` NOT run (maintainer review first).
Note: existing scale-guard baseline entries expire 2026-09-30 (pre-existing).
