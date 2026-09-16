# Config & SOP studio v2 (static prototype)

Run from this folder:

```sh
python3 -m http.server 4391 --bind 127.0.0.1
# open http://127.0.0.1:4391/
```

No build step. State lives in localStorage (`mesha.config-sop-studio.v2.state@4`); user menu → Reset data restores the seed.

Routes
- `#/configuration/items/<register>` — Items and settings (parks, pens, partitions, farms, species, breeds, sexes, stages, groups, healthStates, animals, items, people, roles, approvers, vendors, trucks, reference lists, settings)
- `#/configuration/items/park/<id|new>` — park with pens grid
- `#/configuration/items/animal-types/<id|all|new>` — several species together
- `#/configuration/items/sheet/<register>` — spreadsheet entry / bulk edit
- `#/configuration/items/import` — upload → match columns → dry run + review grid → done (undo)
- `#/configuration/work-instructions` — SOP list; `/<sopId>` flowchart, `/<sopId>/list`, `/<sopId>/operator`
- `#/configuration/work-instructions/master/<id>` — master SOP stages; `/try` for a try run
