# Source Findings Reference

Load this when using facts from General/Slack docs, customer promise safety
material, legacy dashboards, Slack/App Script, or when checking whether source
facts reached canonical docs.

Canonical docs:

- `context/source-findings/drive-docs-findings.md`
- `context/source-findings/customer-promise-safety-findings.md`
- `context/source-findings/feed-transfer-kt-2026-06-24.md`
- `context/source-findings/feed-direction-workbook-automation-findings.md`
- `context/product/glossary.md`
- `context/analytics/final-analytics-infra.md`
- `context/forms/final-forms-sop-engine.md`

Rules:

- Commit sanitized summaries only. Do not commit raw Drive files, raw Slack
  exports, contacts, phone numbers, local filesystem paths, media URLs, tokens,
  screenshots, or private rows.
- If a source fact affects build behavior, it must land in an authoritative
  context doc, not only in analysis or archive.
- For Feed Direction, `Feed, Shiftings and Count.docx` v1.1 is the controlling
  business source for timing, Diff, bridge handling, one-day projection, Base
  Count adoption, as-fed quantities, and ration constraints. Legacy Slack/App
  Script trigger times are audit/cutover evidence only and must not become
  GoatOS schedules unless explicitly owner-approved against that doc.
- The docx default session policy is two serving slots, `09:00` and `15:00`,
  split `50/50`, but it labels that split a deliberate simplification to revisit
  if breed + tag needs an uneven split. GoatOS should model Feed Direction
  sessions as versioned admin config so approved users can add, disable, reorder,
  or reweight slots through an effective-dated published protocol version.
- Feed Transfer KT notes support uploaded/configured breed, tag/stage, energy,
  weight-band, warm-up, pregnancy, and feed-vector constraint tables, but they do
  not provide authoritative shed placement. Keep physical count projection
  (`shed + breed`) separate from nutrition/ration cohort keys; missing reviewed
  resolver context must block Feed generation instead of guessing.
- `Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` are
  migration/parity evidence, not target schema. Their tabs, formulas, hidden
  copies, processed flags, and legacy Apps Script glue must be replaced by
  typed imports, CRUD/review/publish config, immutable generation snapshots,
  stage obligations, proof/rework, audit/outbox, and bounded Postgres reads.
- Deleted archive docs and old phase ladders are historical only. If a breed,
  table, SOP, or form field only exists in git history, it is not
  build-canonical.
- Source facts captured so far include:
  - CBE/CPT/CJB/BLR aliases and old-tag scope.
  - RFID source snapshot aggregate counts, RFID uniqueness, old-tag duplicate
    review cases, and import mapping rules.
  - HF/origin/source semantics and shared-pending ownership nuance.
  - status/stage meanings, F2 sex-contamination rule, Warmup, M0.
  - breed/species seed labels and alias requirement.
  - reproduction parameters for Phase 6.
  - Slack form schemas and health symptom option sets.
  - legacy BigQuery/dashboard table catalog.
  - Customer promise safety decision logic and residual safety gaps.
- If a new source doc is reviewed, update `context/source-findings/` and any
  affected product/architecture/forms/analytics doc in the same commit.
