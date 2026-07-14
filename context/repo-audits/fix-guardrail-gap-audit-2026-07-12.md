# Fix Guardrail-Gap Audit — "Fixed Without a Guard"

**Date:** 2026-07-12 (Asia/Kolkata)
**Repo HEAD at audit:** `6347a98f`
**Source ledger:** `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`
**Method:** 4 parallel subsystem auditors (backend / admin-web / android / ci-systemic). For every row the ledger marks **FIXED WITH PROOF**, verify three closure criteria at current HEAD:

- **(A) Repro / E2E test** — a test that fails before / passes after; production-path E2E preferred (not a seeded readback).
- **(B) Local-CI proof** — the relevant `make ci-local` gate was run on the pushed SHA.
- **(C) Recurrence guardrail** — a *machine-enforced* guard (guard script wired into `make guardrails` / `ci.yml`, a scale/mobile/defer guard entry, a CI job, or an enforced ADR lint) so the **bug class** cannot silently return.

This file is a NEW audit output. It does **not** edit the closure ledger (coordinator owns that) and fixes no product code (STOP RULE).

---

## Headline

The complaint is **valid**. Almost every fix shipped with a test + local-CI proof (good), but **the fix class was rarely wired into a machine guard**, so the same defect can re-enter unblocked.

| Criterion | Backend (10 rows) | Frontend (5) | Android (4) | Verdict |
|---|---|---|---|---|
| (A) has repro/E2E test | 10/10 (1 schema-only) | 3/5 real, 2 weak | 1 solid, 3 partial/none | mostly OK |
| (B) local-CI proof on SHA | 10/10 | assumed, Android never | Android gate never ran on their SHA | OK backend, **hole mobile** |
| (C) recurrence guardrail | **2/10** | **1/5** wired (regex) | **0/4** | **systemic hole** |

**Only 2 of the whole ledger have a real class-guard: C35-003 (`sweeper-deployment-guard`) and C35-010 (`clinical-defer-guard`).** Everything else is a one-off patch + test.

---

## A. Fixed-without-a-guard rows (per subsystem)

### Backend (auditor 1)

| Row | Fix SHA | A test | B CI | C guard | Sev | Missing guard to add |
|---|---|---|---|---|---|---|
| C35-003 sweeper actor | `ed17a246` | ✓ | ✓ | ✓ | CLOSED | — |
| C35-010 clinical defer P0 | `56e0e804` | ✓ | ✓ | ✓ | CLOSED | — |
| **C35-004** bulk sweeper N+1 write | `10264732` | ✓ | ✓ | ✗ | **P1** | scale-guard doesn't scan `backend/cmd/**`; sweeper loop `obligation-sweeper/main.go:358-367` unguarded. Add cmd-path scan + txn-count assertion at 100/1k batch. |
| **FIXCHK-001** app-vaccination park scope | `04cd330c` | ✓ (units) | ✓ | ✗ | **P1** | units only; no full E2E middleware→handler→SQL clamp proving cross-park denial. |
| C35-007 finalization index | `10183f15` | schema-only | ✓ | ✓(sqlc-plan) | P2 | no envelope-scale (~500k-row) EXPLAIN E2E; sqlc-check passes at small scale (1M+ is future certification per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`). |
| C35-023 unbudgeted prune | `539284b5` | ✓ | ✓ | ✗ | P2 | no envelope-scale (~500k-row) prune budget/checkpoint/restart test (million-row prune is future certification); the prune must be budgeted/checkpointed regardless. |
| C35-024 consumer replay | `2f5dfce8` | ✓ | ✓ | ✗ | P2 | only vaccination handler tested; no replay-safety registry across all consumers. |
| NEW-E2E-001 control-tower projector | `6d5469cd` | ✓ RED→GREEN | ✓ | ✗ | P2 | no pinned-clock boundary matrix (before/at/after 14-day cutoff). |
| FIXCHK-002 FEFO UTC vs IST | `d682d28c` | ✓ (1 case) | ✓ | ✗ | P2 | no IST-midnight frozen-clock matrix; **no timezone lint** (class open). |
| C35-025 operations keyset | `e1af5d17` | ✓ | ✓ | ✗ | P3 | no HTTP/UI cursor E2E (backend proof exists). |

### Admin-web / calendar (auditor 2)

| Row | Fix SHA | A test | B CI | C guard | Sev | Note |
|---|---|---|---|---|---|---|
| **C35-013** Action Center dual-fetch + OFFSET | `682b8866` | ✗ frontend | ? | ✗ | **HIGH / PLAUSIBLE live** | backend keyset done, but frontend still issues both tab requests in one `Promise.all`; no fetch-count test. **Verify — possible still-open bug, not just guard gap.** |
| C35-015 SOP N+1 fanout | `40672185` | ✓ backend | ✓ | ✗ | MED | no SSR load test at N=200; no guard vs new `Promise.all` fanout in admin-web. |
| C35-014 calendar marker overlap | `1b985cbf` | regex guard | ✓ | ⚠ | MED | regex-only; no runtime test that closed picker makes zero marker calls. |
| C35-016 serial-await fanout guard | `64f55997` | ✓ self-test | ✓ | ✓(regex) | LOW | accepted; static-only limit noted. |
| FIXCHK-003 weak cursor assertion | `6dc76da3` | ✓ | ✓ | n/a | LOW | now decodes+validates snake_case key; accepted. |

**Cross-cut:** `scale-guard` is Go-only (scans `backend/internal`); it does **not** scan admin-web TS. `mobile-guard` (`check-mobile-list-fetch.mjs`) targets Android/`.kt`. So the "capped read-time rollup / OFFSET / fanout" ban is **prose-only for admin-web** except the one regex fanout guard.

### Android (auditor 3)

| Row | Fix SHA | A test | B CI | C guard | Sev | Note |
|---|---|---|---|---|---|---|
| C35-001 logout clean-slate wipe | `c22764d5`/`5ca5166d` | ✓ reset test | gate exists | ⚠ partial | P1 | wipe implemented + tested, but **no inventory guard** — a future Room/DataStore/WorkManager store added without adding it to `LogoutCoordinator` silently leaks; nothing catches it. |
| C35-011 record verify/rework | `f1f6a5be` | ⚠ mock only | compile | ✗ | P1 | no ViewModel action test (button→outbox+idempotency). |
| C35-019 record offline-first single read | `f1f6a5be` | ✗ | compile | ✗ | P2 | no cold-offline test; nothing blocks re-introducing the `repo.rows()` network fallback. |
| C35-006/018 ScanViewModel `limit=1000` | (unchanged) | ⚠ | **skipped** | ✗ | P1 | **mobile-guard is diff-scoped in CI** → ScanViewModel wasn't in any diff, so the whole-tree offender `ScanViewModel.kt:58,80 limit=1000` never got scanned. Live anti-pattern, green CI. |

---

## B. Systemic coverage matrix (bug class → machine guard)

| Bug class | Guard | Wired in `guardrails`/`ci.yml`? | Coverage gap |
|---|---|---|---|
| compute-on-read / god-CTE | `scale-guard` | ✓ | `backend/internal` only; 51 grandfathered baseline (ratchet, not certified) |
| N+1 write / OFFSET / non-SARGable | `scale-guard` | ✓ | **misses `backend/cmd/**`** (C35-004 sweeper loop live) |
| mobile over-fetch / pagination | `mobile-guard` | ✓ | **diff-scoped** → pre-existing offenders (ScanViewModel:1000) never scanned |
| clinical defer set | `clinical-defer-guard` | ✓ | **STRONG** (whole-file scan + 20 self-tests) |
| sweeper actor identity | `sweeper-deployment-guard` | ✓ | JS-only; **Terraform never validated** the env var is wired |
| E2E must use production paths | `check-e2e-kernel-integrity.sh` | ✓ | **STRONG** |
| Android compile + unit | `android-quality.yml` | ⚠ **path-triggered** | skipped on backend/admin-web PRs; not in the required guardrails job |
| **idempotency write-path contract** | — | ✗ | **PROSE-ONLY** (AGENTS.md mandate, no guard) |
| **atomic state-transition + read-model rollback** | — | ✗ | **PROSE-ONLY** |
| **validate-or-reject authored config** | — | ✗ | **PROSE-ONLY** |
| **India-business-date (no UTC business day)** | — | ✗ | **PROSE-ONLY** (FIXCHK-002 is exactly this class) |
| **offline-first Room SSOT (network-only read BANNED)** | ⚠ mobile-guard partial | ✗ | **PROSE-ONLY** (C35-001/019/MOB-007 are this class) |
| 5k-50k envelope proof + future 1-5M latency/scale certification | targets exist (`api-latency-gate`, `scale-kernel-gate`) | ✗ not on ordinary PR | ratchet-pass ≠ envelope query-plan proof (~500k rows), and neither is the future 1-5M certification (C35-009); see `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` |

**5 mandated AGENTS.md "Do:" rules have ZERO machine enforcement** — idempotency, atomic-rollback, validate-or-reject config, India-business-date, offline-first Room SSOT. Any new code violates them undetected; four of them map directly to bugs we just "fixed."

---

## C. Prioritized guardrails to add (the actual work to close the complaint)

**P1 — stop the live/blocked-CI holes**
1. Extend `scale-guard` scanner to `backend/cmd/**` (catches C35-004 sweeper loop). — `tools/scale-guard/scaleguard.go`
2. Run `mobile-guard` **whole-tree** (`--all`) in CI as a required-or-loud-report step, not diff-scoped only. — `.github/workflows/ci.yml` + Makefile `mobile-guard`.
3. Wire the Android compile+unit gate into the always-run guardrails job (JDK/SDK is the only gate; no USB needed). — `ci.yml`.
4. FIXCHK-001 full app-route park-scope E2E (cross-park request → 403 / SQL clamp).

**P2 — enforce the 5 prose-only contracts**
5. `check-offline-first.mjs` — flag any screen ViewModel read wired to a network-only `api.xxx()` pass-through with no Room entity+DAO+Flow (C35-001/019).
6. India-business-date lint — flag `time.Now().UTC()` / UTC day-bucketing in read-time boundary logic; require `Asia/Kolkata` conversion (FIXCHK-002).
7. Idempotency-contract guard — mutating service methods must carry an idempotency key + tests must cover first / exact-replay / different-payload-replay.
8. Atomic state-transition rollback guard — `Publish*`/`Finalize*` paths must upsert+parity-check the owned read model in-transaction and have a rollback regression test.
9. Validate-or-reject config guard — authored-config fields present-but-out-of-range must reject, never silent-default.
10. Add a bounded (10k-row) latency smoke gate to guardrails so scale is shape-proven each PR (C35-009), and mark scale-guard baseline entries `grandfathered` vs `fixed` (C35-020).

**P3**
11. Terraform assertion that the sweeper job sets `GOATOS_SWEEPER_ACTOR_ID` (C35-003 deployment shape).
12. Add HTTP cursor E2E for operations keyset (C35-025) + boundary matrices (NEW-E2E-001, FIXCHK-002, C35-007, C35-023).

---

## Must-verify (not just a guard gap)

- **C35-013** — frontend auditor reports Action Center may **still dual-fetch** both tabs in one `Promise.all` despite the "fixed" mark. Confirm against `apps/admin-web` current source before trusting the closure. If real, this is an OPEN bug, re-open in the ledger via the coordinator.
