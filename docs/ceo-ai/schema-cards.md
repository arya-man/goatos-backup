# CEO AI schema cards

Plan v3 D1.1 (`docs/ceo-ai/plan-v3-one-brain-two-doors.md`). A **schema card** is the
repo-owned, machine-checked description of one `ceo_ai.*` reporting view the
leadership assistant may read through the SQL fallback tier. There is exactly one
card per view, in `backend/internal/ceoai/reporting/schema_cards.go`.

## What a card is

```go
SchemaCard{
    Name:                "mortality_base",            // bare view name, no ceo_ai. prefix
    Purpose:             "Deaths per business day and park with the active population denominator …",
    Grain:               "one row per business day per park",
    DateColumn:          "event_date",                // "" for a current-state view
    ParkColumn:          "park_label",                // "" when the view has no park scope
    TenantScopedColumns: []string{"tenant_id"},
    GroupByColumns:      []string{"event_date", "park_label"},
    Columns:             []Column{{"tenant_id","uuid"}, {"event_date","date"}, …}, // == information_schema
    AggregateOnly:       false,                       // true for per-entity base views
    NeverAverage:        []string{"active_population"},
    Route:               "/counts/mortality",         // admin-web drill-down href
}
```

Field meaning:

| Field | Used by | Meaning |
|---|---|---|
| `Purpose`, `Grain` | planner prompt | what question the view answers, what ONE row is |
| `DateColumn` | `sqlguard.ValidateWindow`, prompt, repair | the business-day `date` column a period binds to. **Empty = current-state view**: a period question on it is answered "as of now" and the answer prints `Window: as of <date>` |
| `ParkColumn` | prompt | the park label/id column the planner may filter on |
| `TenantScopedColumns` | tests | must contain `tenant_id`; the executor binds the session tenant on every read |
| `GroupByColumns` | prompt | dimensions a leadership breakdown may group on (measures excluded) |
| `Columns` | prompt, pg diff test | full ordered `name:type` list; must equal `information_schema.columns` for the view |
| `AggregateOnly` | prompt | per-entity base view (per animal / task / load / obligation): the model must aggregate rather than dump rows. Per-animal answers for the caller's tenant are still allowed by the guard |
| `NeverAverage` | prompt | ratios, medians, percentiles, per-entity caps that must not be re-averaged or summed |
| `Route` | composer | admin-web page offered as the drill link |

Column types use the short spelling `PGTypeShort` produces: `uuid text date
timestamptz bigint integer numeric double boolean jsonb`.

## Where cards are consumed

1. **Planner prompt** — `adapters/vertex/prompt.go` renders `reporting.RenderCardBlock()`
   (one compact line per view, ~95 estimated tokens each, name-sorted) in place of the old
   single-view hint. The rendered system+user prompt is pinned by
   `adapters/vertex/testdata/prompt.golden` (`go test ./internal/ceoai/adapters/vertex -run TestPlanPromptGolden -update` to regenerate deliberately).
2. **Window guard** — `sqlguard.ValidateWindow(sql, card, window)`: when the orchestrator
   resolved a period from the question (`app.ResolveWindow`) and the card has a
   `DateColumn`, the model's draft must contain `<date_col> >= '<from>'` and
   `<date_col> < '<to_exclusive>'` (whole-token match on the same tokenizer as `Validate`).
   With no `DateColumn` it returns `ErrWindowOnCurrentStateView`; the orchestrator strips
   the window before execution (`prepareSQLWindows`) and the composer prints
   `Window: as of <date>`.
3. **Repair loop** — on a sqlguard reject or a Postgres error of a model-drafted
   `sql_fallback`, the orchestrator re-prompts Vertex once (`RepairSQL`) with the error
   text and `card.RenderCompact()` of the referenced view, re-runs the corrected draft
   through the same guard, and on a second failure takes the honest-partial path.
   Counters: `ceoai_sql_reject_total{reason}` and `ceoai_sql_pg_error_total{code}`; the
   validator reason lands in the admin trace step.

## How to add a card (new `CREATE OR REPLACE VIEW ceo_ai.<name>`)

1. Write the view migration with its `projection-review:` header (grain, join cardinality,
   scope) — the card's `Grain`/`Purpose` are derived from it.
2. Add a `SchemaCard{…}` entry to `schemaCards` in
   `backend/internal/ceoai/reporting/schema_cards.go`. Copy the column list from the view
   SQL **in projection order**; the Postgres-gated diff test compares both directions.
3. Decide `DateColumn`: the view's business-day `date` column when rows are periodised
   (deaths per day, feed per feed day, obligations per due day). Leave it empty for a
   current-state view (occupancy now, open queue now). Never point it at a `timestamptz`.
4. List `NeverAverage` for any ratio/average/median/percentile/cap column.
5. Run:

```bash
cd backend && go test ./internal/ceoai/reporting ./internal/ceoai/adapters/vertex
# then regenerate the prompt golden if the new card changed it:
go test ./internal/ceoai/adapters/vertex -run TestPlanPromptGolden -update
# Postgres-gated column diff (explicit opt-in; uses the pgtest harness):
GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_PGTEST_ADMIN_DSN=… go test ./internal/ceoai/reporting -run TestSchemaCardsMatchInformationSchema
make ceo-ai-schema-card-guard
```

Changing an existing view's columns (even append-only) means updating its card in the
same change — `TestSchemaCardsMatchInformationSchema` fails on any drift.

## The guard

`make ceo-ai-schema-card-guard` runs `tools/agent-hooks/check-ceo-ai-schema-cards.mjs`
(self-test first). It scans every `CREATE OR REPLACE VIEW ceo_ai.<name>` in
`backend/migrations/postgres/*.sql` (Up sections only; case- and whitespace-tolerant) and
fails if `schema_cards.go` has no `Name: "<name>"` card. It is registered in
`tools/ci/guardrail-manifest.json`, `Makefile:guardrails` and `tools/ci/run-local-ci.sh`
(`run_common`). The Go twin `TestSchemaCardsCoverEveryMigrationView` catches the same
omission under `go test`.

Tests that pin the contract:

- `TestSchemaCardsMatchInformationSchema` (Postgres-gated; skips cleanly without a DB)
- `TestSchemaCardsNoBannedKeywordColumns` — no card column equals a `sqlguard.BannedKeywords()` entry
- `TestEveryCardHasTenantIDColumn`
- `TestSchemaCardsInternalConsistency`, `TestSchemaCardsCoverEveryMigrationView`, `TestRenderCardBlockIsBounded`
- `sqlguard`: `TestValidateWindowRequiredDateLiterals`, `TestValidateWindowCurrentStateViewRefusesPeriod`
- `app`: `TestResolveWindowTable` (55 phrases), `TestRepairLoopOnceThenPartial`,
  `TestWindowThreadedIntoSQLAndEnforced`, `TestWindowOnCurrentStateViewAnswersAsOfNow`
