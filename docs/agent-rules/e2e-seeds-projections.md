# E2E, Seeds, Migrations and Projection Closeout

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

Do:

- Keep architecture facts in `context/`.
- Treat every test or script labeled E2E as a production-path proof, never a
  seeded readback. E2E fixtures may insert only external/input facts required to
  start the scenario (for example tenant, herd animal, location, workforce,
  inventory, or authored configuration). Obligations, batches, completions,
  verification outcomes, SOP tasks/submissions, notifications/escalations,
  cancellations, and Calendar/process-integrity screen output must be produced
  by the same service, API, durable event consumer, sweeper, canonical-read
  query path, or projector used in production. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the Calendar,
  process-integrity, and vaccination shed/execution/operations screens are
  served at the current 5k-50k envelope directly from canonical indexed SQL —
  the `calendar_event_projections`, `process_integrity_projection_rows`, and
  `vaccination_shed/execution/operations_projection_rows` projection tables are
  retired, not replaced by a seeded stand-in. E2E for those screens must still
  drive the real canonical-read path end to end; if a screen later earns its
  own projection under that ADR's scale-out ladder, this same production-path
  requirement carries over to that projector. A narrower test that
  intentionally seeds derived state must live with the owning package as an
  integration/read-model test and must not appear in an E2E report.
  `tools/agent-hooks/check-e2e-kernel-integrity.sh` enforces this rule for both
  Claude and Codex and in CI.
- Couple migrations to initial seed setup. If a migration changes tenant/goat/
  RFID/location, HRMS/ownership, founder grants, protocol/SOP/capacity,
  obligation/completion/proof, notification/verification, or app-visible
  projection/read-model tables, update the matching seed command,
  seed/projection test, or seed runbook in the same patch. The
  `seed-migration-guard` target is part of `make guardrails` and blocks
  schema/read-model drift where source rows seed correctly but the live app reads
  empty or missing projection tables. See
  `docs/runbooks/initial-seed-migration-coupling.md`.
- Do not make seed scripts hand-fill every new table. Classify setup tables as
  source/canonical, derived/read-model, static catalog/config, or
  operational/audit/event. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the default at
  the current 5k-50k envelope is that a new app-visible surface is served by a
  canonical indexed SQL read — no new projection table, and no closeout wiring,
  for that default case. A derived/read-model table exists only where it
  survives this envelope (the vaccination eligibility rollup and counts
  summaries) or where the ADR's scale-out ladder later adds one for a specific
  measured hot read. Any such surviving or newly-added projection table must
  still be rebuilt from canonical data through `make seed-closeout` /
  `tools/dev/seed-closeout.sh`, and still needs access-pattern indexes,
  freshness/version state, and an explicit partitioning decision at the point
  it is introduced.
- Register projection closeout by app-visible output, not just by executable
  name. If one projector command owns multiple read models, `seed-closeout`
  must pass explicit flags for each output. Any default-false `-project-*` flag
  for a visible read model must appear as `-project-...=true` on the owning
  command invocation in closeout, and the guard must verify the executed
  `tools/dev/seed-closeout.sh --dry-run` output rather than raw shell text. A
  commented, disabled, or uncalled invocation does not count, or the seed can
  claim the projector ran while leaving that table empty.
- Projection-backed operator pages must follow the last-known-good serving
  contract. No first projection, no serving rows, or a requested window outside
  projected coverage may fail closed. A stale/yellow/rebuilding/failed/over-TTL
  projection that still has serving rows covering the request must serve those
  rows with freshness metadata instead of taking the page down. See
  `docs/decisions/high-scale-dashboard-projections.md`.
- Source-backed vaccination seed means the whole executable setup, not goats
  alone: founder grants, HRMS roster, attendance/leave, timetable-backed
  positions, strict shed manager/backup mapping, position duties, published
  `vaccination.matrix` config, trusted vaccination history, generated future
  obligations, generated drive batches, and deterministic closeout. Missing
  HRMS/config is a failed seed, even when goat rows exist. A reseed/import/local
  proof is also failed if it stops after generation and leaves visible-window
  `scheduled`/`due` vaccination obligations unbatched; `tools/dev/seed-closeout.sh`
  must run the obligation sweeper and fail on that condition.
- Every accepted live goat in seed/import/dev data must resolve to a real active
  shed. During the current build phase, missing source placement is completed
  deterministically into an explicit seed-intake park/shed; do not skip the
  animal, leave `shed_id` blank, or fall back to a park/tenant vaccination
  obligation. Goat vaccination obligations are **shed-scoped only**; park is
  the drive execution/grouping scope. Required guards:
  `make goat-shed-scope-guard`; post-seed DB proof:
  `make goat-shed-integrity-db-proof` or `tools/dev/seed-closeout.sh`.
