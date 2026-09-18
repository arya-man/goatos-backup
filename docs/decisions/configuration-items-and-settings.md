# Configuration: Items and settings

Maintainer instruction 2026-09-18, from the Claude prototype merged in #297
(`docs/prototypes/config-sop-studio-v2/`, route `#/configuration/items`). Phase 1 of the
Configuration vertical; Work instructions (the SOP canvas) is phase 2.

## Decision

The farm's reference lists — farm places, animal types, catalogues — stop being seed literals
and become tenant-scoped **registers** a person edits on screen at `/configuration/items`.
The target stated by the maintainer: *everything configurable*, so a new owner adds their own
animal types, places, feeds, medicines and categories without a developer.

Phase 1 registers (maintainer's picks): **Farms, Parks, Pens, Partitions** (farm places);
**Species, Sexes, Lifecycle stages** (animal types); **Item categories, Items, Feed items**
(catalogue; feed items read-only, edited on `/feed/config`). Inventory stock, breeds, the
animals grid, people/roles and sales rules are later phases.

Access: `configuration.read` / `configuration.write`, `ceo_internal` on the role; anyone else
by a per-person tick on `/people` (module `configuration`, web only, View / Configure).

## Shape: a register is data, not a screen

`backend/internal/configuration/domain.Registers` is the catalog. One `Register` names its
columns, their types (`text`, `code`, `number`, `bool`, `enum`, `ref`, `notes`), which
register a ref points at, hints, required/immutable flags, and which columns filter. The HTTP
contract serves those definitions (`GET /admin/configuration/registers`) and the admin-web
page renders every register from them. Adding a list is one definition plus one store; never a
new page. Every visible word on the screen comes from the definition or the page contract.

Rows have one shape on the wire (`ConfigurationRow`): `id`, `display`, `status`,
`row_version`, `is_builtin`, `fields`, `labels` (ref targets' names), `counts` (what the row
holds). Writes send `fields` only; the backend validates against the definition and returns
per-field refusals (`field_errors`) the drawer shows under the input they name.

## Storage: the tables the product already reads

No second copy of a place or an item:

| Register | Tables |
| --- | --- |
| Farms / Parks / Pens | `locations` (type farm / park / shed) + `farm_profiles` / `park_profiles` / `shed_profiles` |
| Partitions | `shed_partitions` (id on the wire is `shed_id:normalized_label`; the table has no surrogate key) |
| Species / Sexes | `species_lookup` / `sex_lookup` (migration 000346) |
| Lifecycle stages | `animal_stage_lookup` |
| Item categories | `item_categories` (000346), an editable tree over `inventory_items` |
| Items | `inventory_items` (+ `vaccines` for a vaccine's facts; kind-specific facts in `context`) |
| Feed items | `feed_item_catalog`, read-only here |

A pen's `location_code` is derived (`<PARK>_SHED_<NAME>`, the seeded shape), which is how two
pens of one park cannot share a name. The legacy `Castro 1`-style alias rows are excluded from
every pen list and count through `oploc.PartitionAliasExclusionSQL`. A partition's display is
composed in Go through `oploc` (never in SQL) so it reads `Godel 1 - Part 3` here exactly as
everywhere else.

## Species and sex are the farm's vocabulary now

`goats.species` and `goats.sex` were CHECK constraints (`goat|sheep`, `female|male`). 000346
drops them; the per-tenant lookup is the rule. It is deliberately **not** a foreign key: goats
carries no FK to tenants and 86 integration fixtures insert goats under ad-hoc tenants with no
tenants row. The identity create/import path validates the shape in the service and membership
in the repository against the lookup (`lookupCodeAllowed`); a tenant with no lookup rows at all
still accepts the four built-ins.

The four built-in rows (`goat`, `sheep`, `female`, `male`) are flagged `is_builtin`: they may
be renamed but never archived or deleted, because vaccination, feed and weighing rules still
name them by code. **A newly configured species gets no vaccination or feed rule until someone
authors one** — an honest boundary, stated on the screen's hint.

Still hardcoding the four literals (follow-up, not in this change): the procurement load-goat
entry (`procurement/app.service.go`) and the census sex correction
(`identity/app/census_correction.go`).

## Archive, delete, built-ins

Archive is the ordinary exit: the row stays so everything that already names it keeps
rendering; it stops being offered for new rows. An archive is refused (`in_use`, 409, with the
usage sentence — "In use by 12 animals, 3 partitions") while dependents still name the row;
the usage check runs inside the write transaction. Delete is for a row nothing ever used and is
refused the same way; a foreign key nobody counted (history rows) turns into the same refusal
rather than a 500. Built-in rows offer rename only.

Capability lock, both halves: the four page-contract controls (`create_row`, `edit_row`,
`set_row_status`, `delete_row`) on `configuration.write`, and the route table
(`POST` / `PUT` / `DELETE` `/admin/configuration/*`). A reader sees the page with each control
disabled and its reason. Migration 000347 writes the `configuration` web tick for every
migrated `ceo_internal` holder (the 000321 shape): on a real database the person rows decide,
and the page 403'd for the CEO cohort until a row existed.

## Pinned by

- `configuration/domain/rows_test.go` — definition consistency, validation rules, code shape,
  immutability, kind-scoped columns, usage sentence.
- `configuration/adapters/postgres/repository_integration_test.go` — the lifecycle of every
  store on real Postgres (idempotent replay, version fence, usage refusals, alias exclusion,
  partition rename, category tree, vaccine mirror, paging, audit).
- `adminui/app/configuration_page_test.go` — controls gated on write, nav gated on read.
- `permissions/configuration_permissions_test.go` — CEO-only on the role, module ticks, route
  table.
- Browser proof 2026-09-18 on a throwaway stack (API :8101 → `goatos_cfgqa` clone of OCI at
  000347, admin-web :3398): a 26-step Playwright click-through over pens, partitions, species,
  categories, items and feed items, 26/26 green, with rendered screenshots.
