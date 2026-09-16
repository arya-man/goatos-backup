# Config & SOP studio v2 (static prototype)

Run from this folder:

```sh
python3 -m http.server 4391 --bind 127.0.0.1
# open http://127.0.0.1:4391/
```

No build step. State lives in localStorage (`mesha.config-sop-studio.v2.state@5`); user menu → Reset data restores the seed.

Routes
- `#/configuration/items/<register>` — Items and settings (parks, pens, partitions, farms, species, breeds, sexes, stages, groups, healthStates, animals, items, people, roles, approvers, reference lists, settings)
- `#/configuration/items/park/<id|new>` — park with pens grid
- `#/configuration/items/animal-types/<id|all|new>` — several species together
- `#/configuration/items/sheet/<register>` — spreadsheet entry / bulk edit
- `#/configuration/items/import` — upload → match columns → dry run + review grid → done (undo)
- `#/configuration/work-instructions` — SOP list; `/<sopId>` flowchart, `/<sopId>/list`, `/<sopId>/operator`
- `#/procurement/vendors`, `/trucks` — vendors and trucks (Procurement owns them)
- `#/health/config`, `#/feed/config`, `#/vaccination/plan`, `#/sales/config` — read-only goatos-stg readback (2026-09-17)
- `#/<dept>/sops` incl. `#/health/sops`, `#/preventive-care/sops` — SOPs per department
- `#/configuration/work-instructions/master/<id>` — master SOP stages; `/try` for a try run
