# Configuration: Items and settings

Maintainer instruction 2026-09-18, from the Claude prototype merged in #297
(`docs/prototypes/config-sop-studio-v2/`, route `#/configuration/items`). Phase 1 of the
Configuration vertical; Work instructions (the SOP canvas) is phase 2.

## Decision

The farm's reference lists — farm places, animal types, catalogues — stop being seed literals
and become tenant-scoped **registers** a person edits on screen at `/configuration/items`.
The target stated by the maintainer: *everything configurable*, so a new owner adds their own
animal types, places, feeds, medicines and categories without a developer.

Phase 1 registers (maintainer's picks): **Parks, Pens, Partitions** (Farms was dropped the same day: the farm is the tenant) (farm places);
**Species, Gender (register key `sexes`), Lifecycle stages** (animal types); **Item categories, Items, Feed items**
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
| Parks / Pens | `locations` (type park / shed) + `farm_profiles` / `park_profiles` / `shed_profiles` |
| Partitions | `shed_partitions` (id on the wire is `shed_id:normalized_label`; the table has no surrogate key) |
| Species / Sexes | `species_lookup` / `sex_lookup` (migration 000346) |
| Breeds | `breeds` (product-wide; a breed animals carry cannot be deleted, a rename follows onto `goats.breed`) |
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

## Later additions (2026-09-18, same day)

- **Gender** is the on-screen label of the `sexes` register; key and column unchanged.
- **Items & categories** renders in the prototype's three-column shape: a Lists panel of
  top-level lists (built-in catalogues locked, the farm's own under Your lists), the chosen
  list's sub-lists as chips above its items, feed items folded in read-only from Feed Config,
  a department filter and a tracking chip (both derived on read).
- **People → Roles** edits `designation_catalog`, the list `/people` picks from; RBAC-role codes
  are built in (rename only).
- **Reference lists** (migration 000348): `reference_lists` + `reference_list_entries`, one
  register per list under Reference lists in the rail, plus **Add list** for a vocabulary of the
  farm's own. Seeded: exit reasons (built in; identity's goat exit accepts a farm-added reason,
  never for a death), animal purposes and weight bands (no consumer yet). Status definitions,
  SOP categories and task types have registers over their own tables.
- **Shifting is deliberately out** (maintainer instruction): a movement category is a tag rule,
  not a vocabulary, so it is not a reference list and the raise keeps its seven typed
  categories.

## Phase 2: Work instructions — two kinds of SOP (maintainer instruction 2026-09-18)

Recorded here so it is not rediscovered: from now on, anything built on the SOP side must
distinguish and let the CEO configure **two kinds of SOP**:

1. **Module-level SOPs** — owned by a module and executed inside its workflow (Vaccination,
   Weighing, Feed, Counts / Herd Operations, Procurement, Milk). These are the `/<module>/sops`
   libraries today, keyed by module code prefix.
2. **General SOPs** — not tied to one module: farm-wide procedures (a pen inspection, a daily
   opening routine, a visitor protocol) that stand on their own and can be assigned to people
   or pens without a module owning them.

The Work instructions screen (the prototype's canvas + operator view) must carry the kind as a
first-class, configurable property — where a general SOP lives, who owns it, which task types
and SOP categories it draws from (both editable on this screen) — rather than a naming
convention. This is the design constraint for phase 2; it is not built yet.

## Registers are the pick-lists for every later screen (maintainer question 2026-09-18)

A register here is meant to be SELECTED from elsewhere, not only viewed. The first planned
consumer is the Health Config / Health SOP rewrite: `health_protocol_steps.medicine_name` is
free text today; it becomes a select over Catalogue -> Medicines (`inventory_items`), stored as
`medicine_item_id` plus the label snapshot at publish time (a treated animal stays on the
version it was diagnosed under), with route / strength / unit coming from the item. The
options endpoint every register serves (`GET /admin/configuration/{register}/options`) is the
source for such selects, on the web and on the phone alike; a medicine used by a published
protocol counts as usage and cannot be deleted. Not built yet.

## Bulk download and upload at herd scale (maintainer instruction 2026-09-18)

"Give a bulk upload and bulk download option also, and think of it at a scale ... one lakh
animals." Every register can be DOWNLOADED as a sheet (CSV or Excel) and every importable one
UPLOADED from the same shape, so the round trip is download, fix in a spreadsheet, upload. The
Animals register joins the rail for exactly this: read-only on screen (an animal is edited on
the Herd Register), downloadable as the whole herd, and uploadable through identity's own bulk
pipeline so every herd rule stays where it lives.

**The sheet shape is the register's own columns**, keyed by column KEY in the header (labels
such as "Gender" are matched too), with `id` first and `status` last on a download. A blank id
creates; a kept id UPDATES that row; a sheet of updates may name only the columns it changes. A
ref column carries the target's LABEL on download and accepts label, code or id on upload — a
label two rows share is refused with the ids named, never guessed. Derived columns and `status`
are ignored on upload (archiving stays on screen, where the usage check runs).

**A file is never applied in the request that carried it.** Three phases, each bounded and
resumable (migration `000349_configuration_import_jobs`):

1. STAGE — the upload streams line by line into `configuration_import_rows` in 1,000-row
   COPY chunks; the request holds one chunk, never the sheet. Up to 200,000 rows / 64 MB.
2. VALIDATE — a processor claims the job (`claimed_at` lease, 5 minutes) and walks the staged
   rows in 500-row chunks after a `row_no` cursor: refs resolved once per job, the same
   `ValidateWrite` the drawer runs, in-sheet duplicates. Each row is marked valid/invalid with
   its messages; the job becomes `previewed`. The screen shows counts, the first problems, and a
   downloadable "rows to fix" sheet in the upload's own columns.
3. APPLY — on an explicit click, the valid rows are written through the ORDINARY service
   (Create or Update) under an idempotency key of `(job, row_no)`. A worker that dies mid-chunk
   resumes after the last row it finished; a row it had written but not marked replays by key.

Where it runs: the API kicks a bounded goroutine per upload and per apply so a small sheet
previews in seconds; the kernel worker's `ConfigurationImportStage` (operational lane) sweeps
jobs whose claim lapsed and finishes them. Both claim first, so a job is never worked twice at
once, and a phase end is fenced on the job still being in that phase — a cancel that lands
mid-chunk wins, and rows it marked skipped are not re-marked by the chunk in flight (both
mutation-tested).

**Animals** are the one register the importer does not write: each 500-row chunk is handed to
identity's `PreviewAdminGoatBulkImport` (validate) and preview + `CommitAdminGoatBulkImport`
(apply) with park and pen resolved here by code or name. Identity's per-row rules (identifier
ownership, partition grain, stage vocabulary, birth needs a dam) come back as the row's
messages. Create-only: an animal already on the register is reported, never changed.

Measured on the throwaway stack (OCI over an SSH tunnel, ~150 ms per round trip): a pens sheet
of 4 rows previews in under 2 s; 1,681 animals export in ~0.5 s; animals validate at ~500 rows
per 2 minutes because identity validates each row with its own queries, which over that tunnel
is round-trip bound. On a co-located database (~1 ms) the same arithmetic puts a 100,000-row
animals sheet at a few minutes of worker time, which is what the job shape is for; it was NOT
proven at that size here.

Known edge, accepted: if a worker dies after identity committed an animals chunk but before
the rows were marked, the re-run's preview reports those tags as already owned and marks them
failed although they were created — the sheet's "rows to fix" then names animals that exist.
The register writes have no such gap (the idempotency key replays the original result).

## Pinned by

- `configuration/domain/rows_test.go` — definition consistency, validation rules, code shape,
  immutability, kind-scoped columns, usage sentence.
- `configuration/adapters/postgres/repository_integration_test.go` — the lifecycle of every
  store on real Postgres (idempotent replay, version fence, usage refusals, alias exclusion,
  partition rename, category tree, vaccine mirror, paging, audit).
- `adminui/app/configuration_page_test.go` — controls gated on write, nav gated on read.
- `permissions/configuration_permissions_test.go` — CEO-only on the role, module ticks, route
  table.
- `configuration/domain/bulk_test.go` — sheet columns, header matching (keys, labels, update
  sheets), blank cells, cell rendering, ref resolution and ambiguity.
- `configuration/adapters/postgres/import_integration_test.go` — stage / preview / apply on
  real Postgres, update-by-id round trip, update-only sheets, XLSX template read-back, the
  cancel fences (mutation-tested) and the recovery sweep.
- Browser proof 2026-09-18 (sheets): an 18-step Playwright click-through — download CSV /
  XLSX / template, upload with problems, rows-to-fix download, apply, table refresh, update by
  id, the animals register download + upload through the herd pipeline, Escape close, phone
  width — 18/18 green with rendered screenshots.
- Browser proof 2026-09-18 on a throwaway stack (API :8101 → `goatos_cfgqa` clone of OCI at
  000347, admin-web :3398): a 26-step Playwright click-through over pens, partitions, species,
  categories, items and feed items, 26/26 green, with rendered screenshots.
