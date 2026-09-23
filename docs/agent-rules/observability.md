# Observability, Telemetry and Hot-Path Performance

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

- Add observability for new APIs/workers: latency, errors, DB pressure, queue lag, DLQ, and media failures.
- Goat identifiers (RFID, old tag, breed, farm, shed, partition) are operational
  livestock business data, NOT PII. Log them in diagnostics so a failure is
  traceable to the exact goat/row. The only logging redaction rule is secrets:
  never log credentials, tokens, or service-account JSON. Repo hygiene is
  separate and still applies: do not commit raw private source files or row
  dumps to git.
- Exception: Goat OS STG tester/demo login credentials that are intentionally
  documented in `docs/runbooks/stg-operator-login-credentials.md` are approved
  committed runbook data, not a review finding. Do not flag those STG
  email/password rows as leaked secrets unless the maintainer says they are no
  longer approved, they include production credentials, or they expose tokens,
  service-account JSON, API keys, private keys, or other non-demo secrets.
- Construct backend loggers via `backend/internal/platform/observability`
  (env sink `GOATOS_OBS_SINK`: `stdout_json`/`otlp`/`gcm`); do not hand-roll
  `slog.New` in new code. Log once at boundaries with trace/request/tenant/
  import_run_id context, and recover-and-log panics at goroutine edges. See
  `docs/decisions/observability.md`.

## TELEMETRY GUARDRAIL (mandatory)

Whenever you add or modify a user-facing surface — an Android screen,
viewmodel, or flow in `apps/goatos-android`; an admin-web route in
`apps/admin-web`; or a new product-meaningful event emitted anywhere — you
MUST wire:

1. **Firebase Analytics event(s)** — `AnalyticsPort.track(...)` with an
   `AnalyticsEvents` constant (never an inline string) on Android; a
   Faro event (`faro`/`trackEvent`/`pushEvent`) or route error-boundary
   coverage on admin-web.
2. **Crashlytics fatal + non-fatal logging** on error paths that surface can
   hit (Android; wiring itself is tracked as TODO — see the doc below).
3. **The relevant funnel/journey step**, when the surface sits on a tracked
   journey (`login → bootstrap → drive-open → scan → vaccination-capture →
   submit`, or a future documented funnel).

Run `make telemetry-guard` (or `python3 tools/telemetry-guard/telemetry-guard.py`)
before committing — it is part of `make guardrails`, the compatibility
`make ci-local JOB=guardrails`, and the affected admin-web/Android component
jobs. It is diff-scoped against `origin/main` so unrelated commits pass instantly.

Use `// telemetry:exempt <reason>` only with a real justification (internal
debug-only screen, pure presentational component, route fully covered by a
parent error boundary) — it is a reviewer-facing escape hatch, not a rubber
stamp.

Full rule, rationale, required symbol names (including what is wired today vs
TODO), compliant/non-compliant examples, and how the guard works:
`docs/observability/TELEMETRY_GUARDRAILS.md`.
<!-- END TELEMETRY GUARDRAIL -->

**Whole-tree enforcement note (not part of the generated block above — do not
let a regen strip this):** `telemetry-guard` and its sibling `exception-guard`
("never swallow an exception" — same doc, §8) are diff-scoped by design, but
that is NOT the whole enforcement story. Pre-existing whole-tree debt is
enforced separately by a shrink-only ratchet — `make exception-guard-ratchet`
and `make telemetry-guard-ratchet`, both wired into `make ci-local` via
`guardrails` — that fails if a NEW violation appears anywhere in the tree
(not just on diff-touched lines) or if the checked-in baseline
(`tools/exception-guard/baseline.json`, `tools/telemetry-guard/baseline.json`)
goes stale relative to fixes. **Adding an entry to a baseline file to make a
new change stop failing the ratchet is not an accepted way to land code** —
fix the violation or add a genuine `exception:exempt`/`telemetry:exempt`
marker instead. Full mechanism, the reason baselines are keyed by
file+rule-kind and never by line number, and how to add a ratchet for a
future guard: `docs/observability/GUARDRAIL_RATCHET.md`. The lesson recorded
there: **a diff-scoped guard, by construction, silently permits unlimited
pre-existing debt unless it is paired with a whole-tree ratchet like this
one** — an adversarial audit found ~150 `exception-guard` FAILs and 51
`telemetry-guard` FAILs sitting in this tree with a permanently green
`ci-local` before this ratchet existed.

## Goat OS Hot-Path Performance Guardrail

For Goat OS performance, availability, Grafana/APM, or deployment work, use a
local-first loop: test, document, then promote. Do not push to `main`, merge,
land, or deploy to staging until the relevant local or staging-equivalent proof
is green and written in a local progress document with scope, done, pending,
exact tests/E2E, known failures, before/after metrics, judge status, current
SHA, and deployment state. Repeated staging deployments are not an accepted
debugging loop for API latency or observability issues.

Grafana/dashboard work is not a substitute for proving the product APIs and
pages themselves. Before claiming a performance fix, prove the affected route,
payload size, row counts, request fanout, browser route, and failure strings
locally or against a read-only staging-equivalent dataset. If proof is missing,
stale, blocked, or only inferred, say so plainly and do not promote unless the
maintainer explicitly asks to bypass in that same turn.

When editing route-critical backend reads or admin-web/mobile pages that load
Calendar, Vaccination live tracker, Weighing analytics, Feed analytics, Action
Center, Protocol Adherence, or Control Tower, update the matching
`tools/perf/hot-paths.*.json` manifest and run `make api-latency-policy-test`
plus the relevant `node tools/perf/api-latency-gate.mjs --manifest ...` check
against the local API before pushing. Do not leave a new or changed sidebar
route without a p90/p95/p99 guard.

The standing budget for these hot reads is p90 <= 300ms and p95/p99 <= 500ms.
If a read cannot meet that on the local CEO/CXO path pointed at the OCI/STG-like
database, fix the query or serving shape instead of loosening thresholds.
Burst caches are allowed only for read-only analytics summaries and must be
keyed by tenant, authorized parks, date window, pagination, status/filter shape,
and endpoint-specific selectors. Do not cache mutation-sensitive reads such as
calendar action lists or feed stock read-after-write paths unless the same
change also proves correct invalidation.
