# telemetry-guard

Local CI guard that enforces the TELEMETRY GUARDRAIL: every new or changed
user-facing surface must wire product analytics (and, where applicable,
crash/error logging + funnel step tracking) before it merges.

Full rule + rationale: `docs/observability/TELEMETRY_GUARDRAILS.md`.

## Run it

```bash
# Diff-scoped (default): merge-base(origin/main, HEAD)...HEAD, falling back to
# HEAD~1...HEAD, falling back to a full scan if neither resolves.
python3 tools/telemetry-guard/telemetry-guard.py

# Explicit base ref
python3 tools/telemetry-guard/telemetry-guard.py --base origin/main

# Scope to staged changes only (useful as a pre-commit check)
python3 tools/telemetry-guard/telemetry-guard.py --staged

# Full repository audit (backlog view, ignores git diff state)
python3 tools/telemetry-guard/telemetry-guard.py --all

# Machine-readable output
python3 tools/telemetry-guard/telemetry-guard.py --all --json
```

Also wired as `make telemetry-guard` (see the repo `Makefile`) and runs inside
`make ci-local JOB=guardrails` / the `guardrails` CI job.

Exit code is non-zero only when at least one `FAIL` (blocking) finding exists.
`WARN` findings (currently: admin-web) are reported but never fail the run.

## What it checks

Two configurable surfaces, defined in `config.json`:

- **android** (`mode: block`) — any ADDED or MODIFIED
  `apps/goatos-android/**/*Screen.kt` or `**/*ViewModel.kt`, plus any ADDED
  file under an `apps/goatos-android/**/feature-*/` or `.../features/*`
  package, must reference at least one marker from `AnalyticsPort`,
  `analytics.track`, `AnalyticsFunnels`, `AnalyticsEvents`, `CrashReporter`,
  `Crashlytics`, `recordException`, `logNonFatal` — either in the file itself
  or in a sibling `.kt` file in the same directory.
- **admin_web** (`mode: warn`, non-blocking today) — any ADDED
  `apps/admin-web/app/**/page.tsx` or `**/*Screen.tsx` must reference `faro`,
  `trackEvent`, `pushEvent`, `pushError`, or `ErrorBoundary`, in the file or a
  sibling `.ts`/`.tsx` file. Flip `mode` to `block` in `config.json` once the
  per-route Faro event backlog is closed (see the TODOs in
  `docs/observability/TELEMETRY_GUARDRAILS.md`).

## Escape hatch

Add a comment containing the exempt marker (default `telemetry:exempt`,
configurable in `config.json`) anywhere in the file:

```kotlin
// telemetry:exempt internal debug-only screen, never shipped to users
```

```tsx
// telemetry:exempt internal admin-only diagnostics page, not a tracked journey
```

The reason after the marker is not validated for content — only presence — so
use a real justification; this is a human/reviewer-facing escape hatch, not a
rubber stamp. Reviewers should push back on vague exemptions.

## Tuning

Everything path/marker/mode related lives in `config.json` — no code changes
needed to add a marker name, change a glob, or flip a surface from `warn` to
`block`.

## Tests

```bash
python3 -m unittest tools/telemetry-guard/test_telemetry_guard.py -v
```

Builds throwaway git repos under a temp dir to exercise the real diff-scoped
`run()` path (compliant file passes, missing markers fails, `telemetry:exempt`
passes, a sibling file's marker satisfies the requirement, unrelated file
changes are ignored, admin-web gaps warn instead of block). No network, no
external dependencies.

## Files

- `telemetry-guard.py` — CLI entrypoint (invocation name used by `make
  telemetry-guard` and CI).
- `telemetry_guard.py` — the actual logic (underscore, so it is importable by
  the test module).
- `config.json` — markers, path globs, block-vs-warn per surface, exempt
  marker.
- `test_telemetry_guard.py` — stdlib `unittest` suite.
