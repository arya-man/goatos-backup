# Load Tests Agent Context

Read first:

- `../context/execution/env-load-test-and-doc-hygiene.md`

Purpose:

- k6 scenarios, synthetic data, stress reports, and SLO pass/fail checks.

Do:

- Use real-world skew, not uniform fake data.
- Define thresholds before running tests.
- Test operator sync, vaccination campaigns, media proof, idempotency storms,
  outbox relay, dashboards, telemetry, and sweepers.

Do not:

- Do not write-load-test prod.
- Do not produce graphs without pass/fail verdicts.
