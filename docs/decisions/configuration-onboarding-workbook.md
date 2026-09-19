# Configuration: the onboarding workbook (one Excel, one tab per list)

**Status:** implemented 2026-09-19 (maintainer instruction, same day). Extends
[configuration-items-and-settings.md](configuration-items-and-settings.md), whose bulk-sheet
section stays the canonical description of one register's upload; this document covers the
workbook that carries every register at once.

## What the farm gets

A new farm is set up from **one Excel file** downloaded from *Items & settings → Setup
workbook*. It has a fixed tab per list that takes uploads — **Species, Gender, Lifecycle
stages, Parks, Pens, Partitions, Lists, Items & categories, SOP categories, Task types, each of
the farm's own reference lists, Animals** — each tab carrying exactly the header its upload
expects. The farm fills the tabs it needs, leaves the rest empty, and uploads the file **once**.
Every tab is checked before anything is written; the screen shows one table with each tab's
rows / ready / need fixing, a rows-to-fix workbook (one tab per sheet that had problems), and
one **Apply all tabs** button.

**Both ways stay.** The per-register sheet drawer (download, template, upload, update-by-id) is
unchanged and shares every line of the pipeline; the workbook is a bundle of those same jobs.
A farm that only wants to correct its pens still uploads a pens sheet. *Download everything*
exports every list as a workbook in the upload's own shape (id and row_version first) so the
whole setup round-trips: download, edit, upload.

## The shape, plainly

The pasted review of the single-sheet design (2026-09-19) had the sequence right and it is the
sequence the workbook enforces, tab after tab:

1. animal types — species, gender, lifecycle stages (a pen names the stage it is kept for);
2. places — parks, then pens linked to parks, then partitions linked to pens;
3. catalogue — lists, then items under lists;
4. reference vocabularies — SOP categories, task types, the farm's own lists;
5. **animals last**, referencing park, pen, partition, species, gender and stage by name or code.

Breeds, Roles, Feed items and Status definitions are read-only registers authored elsewhere and
take no tab; `breed` on the animals tab stays plain text. Farms are not a register: the tenant is
the farm, parks are the top place.

`domain.WorkbookOrder` is that list. `TestWorkbookOrderRespectsEveryReference` fails the build
if a register is ever listed ahead of a register its ref columns point at — that test caught
pens-before-stages on the first cut.

## How it works

Migration `000359_configuration_import_bundles`: a `configuration_import_bundles` row per
uploaded workbook; each matched tab is an ordinary `configuration_import_jobs` row carrying
`bundle_id`, `bundle_order` and `sheet_name`, plus a new job status **`queued`** — a tab waiting
for the tabs before it. The recovery sweep deliberately never claims a queued tab; only the
bundle orchestrator promotes it, so a pens tab cannot validate before the parks tab did.

**Stage.** The request reads the workbook (stdlib zip/xml reader, every worksheet, shared
strings loaded once), matches each tab name to a register (key, label or singular noun, case and
punctuation ignored), refuses up front — naming the tab — a header missing required columns,
two tabs for one list, a CSV (no tabs) or a workbook with no rows anywhere; ignores and reports
tabs that match nothing; skips tabs with a header and no rows (the template ships every tab);
then stages each tab's rows in 1,000-row COPY chunks under its own job. The first tab starts
`validating`, the rest `queued`.

**Validate, in order.** `ProcessBundle` walks the tabs: promote the next queued tab, run it to
`previewed`, continue. A tab's validation is the single-sheet validation with one addition:

> **A ref resolves against the target's stored rows PLUS the rows the target's own tab will
> create.** Such a value is stored on the validated row as a token, `bundle-row:<job>:<row>`,
> naming the sibling row (a uuid inside, so it can never collide with a real id).

Pens resolve within their park (`domain.ScopedRefIndex`), so "Castro" on the partitions tab for
CBE is the CBE pen and never ambiguous with the CPT one; the same scoping now also governs the
in-sheet duplicate check (`domain.NameScope`: a pen name per park, a partition per pen, a
category per parent), matching the stores. A pending row whose name a stored row already
carries is left to the stored row — the sheet's own row is refused as a duplicate at apply. A
category that only exists on the Lists tab resolves its item kind by walking parents up the
tab. The animals tab may take park, pen, species, gender and stage from sibling tabs: at
preview the herd register cannot see those yet, so its not-found for exactly those facts is set
aside (`animalPending.setAside`) and everything else — tags, dates, in-register duplicates —
is still checked; at apply the tokens have become ids, the codes exist, and the same checks run
in full.

**Apply, in order, one click.** Tokens resolve to the id the sibling row actually wrote. A
child whose parent was not written — invalid, failed, skipped — **fails with the parent's sheet
and row named** (`parent_not_added`) and is never written: there is no pen without its park.
A row of the same tab written moments earlier (a list under a list) resolves through the
chunk's own results, so ordinary registers are written one row at a time with those in hand.
A tab that stops for a reason other than a row (the list vanished, the server errored) stops
the workbook before the next tab starts (`failBundle` cancels the rest). Cancel parks every
unfinished tab; tabs and rows already applied stay applied.

**Resume.** Unchanged in kind: claim lease, progress cursor, row states, per-row idempotency
key. Both new tests kill the worker — mid-validate and mid-apply on a 900-row pens tab — and the
sweep (a different worker, after the claim lapsed) finishes it with every pen written exactly
once. The in-sheet duplicate maps (names, codes, ids, **tags**) are rebuilt from the rows already
validated when a restart resumes mid-sheet, so a duplicate across the restart boundary is
still caught.

## Two hardenings the review asked for

**Whole-sheet duplicate tags.** The herd register checks a tag against the register and
within the 500-row chunk it is handed, so a tag at row 10 and row 70,000 previewed as two valid
animals and the second failed only at apply. `animalValidator` now keeps a job-level tag index
(both tag columns, case and spacing ignored) and marks the later row invalid at preview:
"This tag is already on row 10 of this sheet." It is one map entry per tag — bounded by the tab —
and is rebuilt on resume. Pinned by `TestDuplicateTagIndexSpansTheWholeSheet` and, at volume,
by the load test's far-apart duplicate.

**Apply round trips.** The apply used to claim the job, claim the row, write, record the row
and patch the job **per row** — five round trips before the write. It now renews the claim and
patches the job once per 200-row chunk, claims the chunk's rows in one statement and records
the chunk's outcomes in one statement. The cancel boundary moved with it and is pinned by
`TestApplyFinishesClaimedChunkThenStopsWhenCancelled`: rows a chunk claim moved are finished,
no later chunk starts, rows the cancel skipped are never written.

## What was measured

Run co-located with Postgres on the OCI dev VM (4 vCPU aarch64, the test binary beside the
database — the shape Cloud Run has beside Cloud SQL). Over the laptop's SSH tunnel every write is
round-trip bound (~15 ms each) and the same file takes hours; those numbers say nothing about
the pipeline and are not recorded.

_Filled in from `TestConfigurationWorkbookLoad` (see the table at the end of this file)._

## Positions recorded, not changed

- **No Pub/Sub in the import.** The database is the queue: every row and its state are already
  there, and a broker would be a second place to lose track of them. Rows the apply writes emit
  the ordinary domain events through the outbox; that is where a bus belongs.
- **Animals apply through identity, one row at a time inside 500-row chunks.** That path owns
  the herd rules, audit, idempotency and events; a set-based herd insert would have to
  reimplement them. The measured rate is the cost, and it is paid in a background job.
- **XLSX is read from memory, bounded by the 64 MB upload cap; CSV streams.** A workbook is
  held while its tabs stream out one row at a time (shared strings are the only table kept). For
  a single very large list, CSV through the per-register drawer is the leaner path.
- **A tab carries up to 500,000 rows** (`DefaultMaxImportRows`, raised from 200,000). Nothing
  holds a sheet; the bound is the dedupe maps and the wait.
- **Known edge, inherited from the single sheet:** if a worker dies after identity committed an
  animals chunk but before the rows were marked, the re-run previews those tags as already owned
  and marks them failed although they exist. Register writes replay by key and have no such gap.

## Where things live

| Piece | Path |
| --- | --- |
| Order, tab matching, tokens, scoped refs, name scope | `backend/internal/configuration/domain/bulk.go` |
| Staging, orchestrator, pending refs, apply, animals | `backend/internal/configuration/app/importer.go` |
| Multi-sheet reader / writer, template, export, error workbook | `backend/internal/configuration/app/xlsx_reader.go`, `sheets.go` |
| Bundle SQL | `backend/internal/configuration/adapters/postgres/import_store.go` |
| Routes | `backend/internal/configuration/adapters/http/handler_bulk.go`, `permissions/routes.go`, `contracts/openapi/admin-api.yaml` |
| Screen | `apps/admin-web/features/configuration/workbook-drawer.tsx`, copy in `adminui/app/service.go` |
| Proof | `configuration/adapters/postgres/import_workbook_integration_test.go`, `import_workbook_load_test.go` |
