# Config & SOP studio v2 (static prototype)

Run from this folder:

```sh
python3 -m http.server 4391 --bind 127.0.0.1
# open http://127.0.0.1:4391/
```

No build step. State lives in localStorage (`mesha.config-sop-studio.v2.state@7`); user menu → Reset data restores the seed.

Routes
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
