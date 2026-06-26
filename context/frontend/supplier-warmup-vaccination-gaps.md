# Supplier Warmup + Holding-Farm Vaccination — Closure Status

Date: 2026-06-25
Status: **covered for the current vaccination trigger slice**

This file previously recorded the supplier warmup / Holding-Farm vaccination
gap. The gap is now closed for the approved scope: Procurement Source Entry owns
the write actions, and PHC Vaccination reads the source-entry/HF evidence context
so operators can see why accepted-intake goats should not be double-dosed.

## What Is Built

- Procurement Source Entry captures source goats with `purpose`
  (`breeding`, `fattening`, `non_breeding`, `unspecified`) and `warmup_days`.
- Warmup classification is purpose-specific:
  - breeding: `45-70d`
  - fattening / non-breeding: `0-14d`
  - unspecified: visible fallback asking for purpose.
- Holding-Farm vaccination evidence can be imported and reviewed through real
  backend endpoints:
  - `POST /procurement/source-entry/goats/{goat_id}/hf-vaccination-evidence`
  - `POST /procurement/source-entry/hf-vaccination-evidence/{evidence_id}/review`
- Trusted HF evidence is part of the procurement handoff and vaccination
  generation suppression path.
- `/procurement/source-entry` renders the mock supplier-warmup table shape:
  Load, Holding farm / supplier, Purpose, Animals, Warmup, Tagging,
  Vaccination at HF, Health / Selection, Status.
- `/procurement/source-entry/loads/{load_id}` has real write forms for source
  goats, HF evidence import/review, source health, pre-dispatch decision,
  dispatch, arrival review, and accepted intake.
- `/vaccination` now includes a read-only Supplier warmup / Holding-Farm panel
  before the status matrix. It uses the same mock table shape and links rows
  back to Source Entry for action.

## Ownership Boundary

Write actions stay in:

```text
Procurement -> Source Entry
```

PHC / Vaccination consumes the evidence and displays read-only context. It does
not create a second supplier-warmup action surface.

## Honest Remaining Limits

- Search and advanced source-entry filters still need backend query parameters
  (`q`, source party, holding farm, purpose, HF evidence status). The UI keeps
  mock-style filter controls visible, disabled with reason, and links to live
  Source Entry status chips.
- Media upload/capture is not part of the current admin-web slice. Proof refs
  can be entered; the upload affordance is disabled with an explicit reason.
- Local/dev vaccine baseline is source-derived, not invented: ET/K1/day-21 with
  K2=42. The 2026-06-26 roster expansion pass closed PPR/FMD/HS/BQ as SOP/roster
  labels only; future promotion requires a new source extract with timing/dose/
  booster values plus publishable approval metadata.

## Validation

Verified in the 2026-06-25 closure pass:

- `npm --prefix apps/admin-web run typecheck`
- `npm --prefix apps/admin-web run lint`
- `npm --prefix apps/admin-web run build`
- `npm --prefix apps/admin-web run check:mock-fidelity`
- `git diff --check`
- `go test ./internal/procurement/... ./internal/identity/... ./internal/vaccination/... ./internal/obligation/... ./internal/vaccinationexecution/... ./internal/permissions`
- `GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3311 npm --prefix apps/admin-web run smoke:visual:live`

Latest screenshot proof:

```text
/Users/ravi/mesha/goatos/.codex-goatos-render/admin-web-screenshots/2026-06-25T15-18-51-155Z/desktop-vaccination.png
/Users/ravi/mesha/goatos/.codex-goatos-render/admin-web-screenshots/2026-06-25T15-18-51-155Z/desktop-procurement-source-entry.png
/Users/ravi/mesha/goatos/.codex-goatos-render/admin-web-screenshots/2026-06-25T15-18-51-155Z/desktop-procurement-load-detail.png
```
