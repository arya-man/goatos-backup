# Config & SOP studio — Claude prototype (v2)

Static browser prototype. Two independent prototypes of the same brief live side by side; see [`docs/prototypes/README.md`](../README.md) for the comparison.

| Prototype | Folder | Port |
|---|---|---|
| **Claude** (this) | `docs/prototypes/config-sop-studio-v2/` | 4391 |
| Codex | `docs/prototypes/config-sop-studio/` | 4322 |

## Run

From the repository root:

```sh
cd docs/prototypes/config-sop-studio-v2
python3 -m http.server 4391 --bind 127.0.0.1
# open http://127.0.0.1:4391/#/configuration/items
```

To run the Codex prototype at the same time, in a separate terminal from the repository root: `sh docs/prototypes/config-sop-studio/start.sh` (port 4322).

No build step, no install. Excel templates load SheetJS from jsdelivr (CSV works offline). State lives in the browser's localStorage (key in `js/store.js`); user menu → **Reset data** restores the seed. After pulling new commits, hard-reload (⌘⇧R) — scripts are cache-busted with `?v=` in `index.html`.

## Where to start
- **Configuration → Items and settings** — farm places, animal types (global Female/Male), animals grid + import, catalogues (Medicines / Vaccines / Feed, locked owner department) and your lists, **Inventory** (lots, receive / use / transfer / write off, serial ranges), people and approval chains, **Sales** (price rules, eligibility 35 kg + margin, valuation rates), business settings.
- **Configuration → Work instructions** — SOPs per department on one draw.io-style canvas (drag, connect, reconnect, delete, undo), operator phone view, versions/publish; master SOP stages (procurement purchase → transit → warm-up).

## Data sources
Seed values come from goatos-stg (read-only, queried 17/09/2026), the Drive docs *Goats and Parks*, *Shifting Reports*, *Transit-SOP*, and the Android forms. Proposed SOPs not published in stg are marked Draft. Nothing is written to any backend.

## Routes
- `#/configuration/items/<register>` — Items and settings (parks, pens, partitions, farms, species, breeds, sexes, stages, groups, healthStates, animals, items, people, roles, reference lists, settings; `groups` is shown as Pen tags; old `approvers` links open Approval chains)
- `#/configuration/items/{saleProducts,costKinds,identifierPolicies,designations,approvalChains}` — business rules (register when defined, else read-only table)
- `#/configuration/items/park/<id|new>` — park with pens grid
- `#/configuration/items/animal-types/<id|all|new>` — several species together
- `#/configuration/items/sheet/<register>` — spreadsheet entry / bulk edit
- `#/configuration/items/import` — upload → match columns → dry run + review grid → done (undo)
- `#/configuration/work-instructions` — SOP list; `/<sopId>` flowchart, `/<sopId>/list`, `/<sopId>/operator`
- `#/procurement/vendors` — vendors with goatos-stg record-type counts (no trucks; `/trucks` redirects here); market cities live in `#/sales/config`
- `#/health/config`, `#/feed/config`, `#/vaccination/plan`, `#/sales/config` — read-only goatos-stg readback (2026-09-17)
- Other sidebar leaves (`#/approvals`, `#/verify`, `#/tasks`, `#/work-board`, `#/alerts`, `#/counts/analytics`, `#/counts/breakdown`, `#/weighing/analytics`, `#/sales/*`, `#/feed/analytics`, `#/vaccination`, `#/vaccination/live-tracker`, `#/procurement/{source-entry,feed-purchases,animal-purchases}`, `#/health/analytics`, `#/counts/milk-preparation`, `#/herd-signals`, `#/operations/audit`, `#/people`, `#/leave`) — read-only goatos-stg KPI row (queried 17/09/2026; no environment tag on screen), plus a small table where a cheap count exists (verify, breakdown, loads, animal purchases, audit, people), and the department SOP link
- Below 1180px (and up to 1366px on SOP / master editor routes) the nav is an icon rail (group icon opens its pages) and the Items rail becomes a select; phones keep the drawer
- `#/<dept>/sops` incl. `#/health/sops`, `#/preventive-care/sops` — SOPs per department
- `#/configuration/work-instructions/master/<id>` — master SOP stages; `/try` for a try run
