# Prototypes

Browser-only design prototypes. Nothing here is wired to production or staging backends, migrations, authentication, mobile releases or deployments. All state is browser `localStorage`. Historical review notes inside these folders describe the prototype at the time and do not certify current production behaviour.

## Configuration and SOP studio

Two independent implementations of the same brief (Mesha configuration workspace + SOP / work-instruction editor) were built in parallel by two coding agents so they can be compared. Both are kept as-is; neither is the chosen design yet.

| | Claude prototype | Codex prototype |
|---|---|---|
| Built by | Claude Code | OpenAI Codex |
| Folder | [`config-sop-studio-v2/`](config-sop-studio-v2/) | [`config-sop-studio/`](config-sop-studio/) |
| Port | 4391 | 4322 |
| Start | `cd docs/prototypes/config-sop-studio-v2 && python3 -m http.server 4391 --bind 127.0.0.1` | `sh docs/prototypes/config-sop-studio/start.sh` |
| Open | http://127.0.0.1:4391/#/configuration/items | http://127.0.0.1:4322/#/configuration/items |
| Requirements | Python 3 | Python 3 (Node.js only for `run-checks.sh` judge scripts) |
| Docs | [`README.md`](config-sop-studio-v2/README.md) | [`README.md`](config-sop-studio/README.md), [`REVIEW-2026-09-17.md`](config-sop-studio/REVIEW-2026-09-17.md) |

Run both at once from two terminals; the ports are distinct so their local drafts do not collide. No build, install, `.env` or backend is needed for either. Do not open `index.html` via `file://`.

### Claude prototype — what it covers
- **Items and settings:** farm places (parks, pens, partitions), animal types with global Female/Male and lifecycle stages, animals grid with Excel/CSV import (preview, fixes, undo), inline create for every parent.
- **Catalogues and inventory:** fixed catalogues (Medicines, Vaccines, Feed) with a locked owner department, user lists, inventory as lots and movements with serial ranges and per-park reorder levels.
- **People and business rules:** approval chains, designations, identifier policies, reference lists.
- **Sales:** price rules by species/breed/sex/stage, eligibility (35 kg minimum + margin), valuation rates wired to Weighing, operator view and farm value.
- **Work instructions:** SOPs transcribed from goatos-stg, Drive docs and Android forms (unsourced ones marked Draft), one draw.io-style canvas for every SOP and the master SOP, operator phone view, versions and publish review.

### Codex prototype — what it covers
- **Configuration workspace:** clickable category/record rows, farm and animal registers, form and Excel/CSV Herd Register entry, animal-type templates, archive/restore guards.
- **Business rules:** pricing and weight rules with type/breed/sex overrides, per-kg or per-animal values, minimum weight and allowed shortfall, shared Sales/Weighing checks.
- **SOP editor:** source-backed Feed, Weighing, Procurement and Health examples, branching, operator preview, local publication and composition (stages, prerequisites, approvals, waits).
- **Validation tooling:** `run-checks.sh` runs the `judge-*.cjs` scripts.

### Boundaries (both)
Browser-local persistence only. No production APIs, database migrations, authentication, real operator dispatch, media upload, mobile release or deployment. Prototype pricing/weight checks do not enforce real sales or alter recorded transactions.
