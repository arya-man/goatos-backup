# Source Findings Reference

Load this when using facts from General/Slack docs, assignment material, legacy
dashboards, Slack/App Script, or when checking whether source facts reached
canonical docs.

Canonical docs:

- `context/source-findings/drive-docs-findings.md`
- `context/source-findings/assignment-promise-keeper-findings.md`
- `context/product/glossary.md`
- `context/analytics/final-analytics-infra.md`
- `context/forms/final-forms-sop-engine.md`

Rules:

- Commit sanitized summaries only. Do not commit raw Drive files, raw Slack
  exports, contacts, phone numbers, local filesystem paths, media URLs, tokens,
  screenshots, or private rows.
- If a source fact affects build behavior, it must land in an authoritative
  context doc, not only in analysis or archive.
- Archive docs are historical only. If a breed, table, SOP, or form field only
  exists in `docs/archive/planning-history/`, it is not build-canonical.
- Source facts captured so far include:
  - CBE/CPT/CJB/BLR aliases and old-tag scope.
  - HF/origin/source semantics and shared-pending ownership nuance.
  - status/stage meanings, F2 sex-contamination rule, Warmup, M0.
  - breed/species seed labels and alias requirement.
  - reproduction parameters for Phase 6.
  - Slack form schemas and health symptom option sets.
  - legacy BigQuery/dashboard table catalog.
  - Promise Keeper assignment decision logic and residual safety gaps.
- If a new source doc is reviewed, update `context/source-findings/` and any
  affected product/architecture/forms/analytics doc in the same commit.
