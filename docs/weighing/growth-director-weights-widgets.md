# Growth Director — Weights page widgets

Status: implemented on `feat/growth-director-weights` · endpoint `GET /growth-director/weights` · UI block below the existing Weights dashboard.

## Purpose

Answer the growth questions the live Weights dashboard cannot: which kids are moving toward
sale weight, which shed×breed×sex groups are stalling, whether directed feed is buying growth,
which feed-sheet lines are broken, and how much of all that can be trusted. The existing
dashboard (KPI strip, park strip, gain-by-shed/breed/sex/stage/load, sheds table, losing kids)
is **untouched** — same endpoints, same rollups, same copy.

## Architecture decision — why a new module

`tools/agent-hooks/check-weighing-free-flow-guard.mjs` isolates `backend/internal/weighing/**`:
weighing code may touch only weighing-owned tables, on read paths as well as writes. Joining
`goats`/`goat_identifiers` is banned there (single file-scoped exception:
`weight_demographics.go`, maintainer decision 2026-08-07), and feed tables are banned outright.
Growth Director needs both, so it ships as its own read-only reporting module:

```
backend/internal/growthdirector/
  domain/ ports/ app/ adapters/postgres/ adapters/http/
```

Boundaries that keep this safe (same as the weight-demographics exception): read-only, a
reporting path with no capture/submit/close behaviour, no scan gated on identity, and a tag that
resolves to nothing is counted and reported, never rejected. The guard still passes untouched —
zero diff under `backend/internal/weighing/**`.

Wiring: bootstrap `api.go`, route entry in `permissions/routes.go`
(`adminGetGrowthDirectorWeights`, `WeighingMonitor` gate — same gate as the other Weights
reads), contract in `contracts/openapi/app-api.yaml`, generated client committed.

## Data rules (all queries)

- **Identity** = `lower(btrim(scanned_identifier))`, rows with blank tags excluded.
  `weighing_observations.animal_id` and `mismatch_status` no longer exist (migrations 000078,
  000082) and are never referenced.
- **Tag → animal**: `goat_identifiers.normalized_value = upper(<identity>)` —
  `normalized_value` is UPPER-normalized (identity service), so joining lowercase would miss the
  `(tenant_id, normalized_value)` lifetime-unique index. The join is 0..1 by that index; one-hop
  `goats.merged_into_goat_id` redirect applied. Breed = `COALESCE(NULLIF(btrim(breed),''),
  '(unknown)')`. **Sex comes from `goats.sex` only — never from shed names like `F2-Male`**
  (contaminated signal per glossary).
- **Dedupe** repeat scans latest-wins per identity: `DISTINCT ON` ordered by `accepted_at DESC,
  observation_id DESC` (the 000080 reporting convention).
- `weighing_shed_observations` reads always filter `withdrawn_at IS NULL`.
- `verification_status='rework'` excluded from growth math; `'pending'` included (the backlog is
  disclosed in the trust panel instead of silently zeroing live weeks).
- **ADG** needs ≥2 weighs of the same identity; losses > 0.30 kg/day are treated as implausible
  scans, not slow growth; changes within 3 % of body weight count as flat (gut fill) — both
  gates from `docs/weighing/weight-truth-method.md`.
- **Feed**: `quantity_kg IS NULL` = blocked (no authored ration — a real problem);
  `0.000` = authored zero (milk-fed kids — never a problem). The CHECK constraint enforces the
  split; **no `COALESCE(quantity_kg, 0)` anywhere**. `workflow='experiment'` /
  `head_count_informational=true` rows (Castro) are excluded from per-head math — their kg is
  already a shed total. Head counts collapse via MAX per (shed, feed_day, shed_tag_key,
  breed_key) before summing.
- **Period**: weighing is selected by campaign-week **overlap** (campaigns are week-grain); feed
  by exact `feed_day` range. `period.resolution` in the response discloses this asymmetry.
- **No roster denominators.** The expected-animals table was dropped (000079). Every denominator
  is self-referential: scans seen, identities seen, pairs formed. The UI never renders an
  `x/expected` shape (guard mode 10).

## Widgets and their exact denominator rules

| Widget | Shows | Denominator rule |
|---|---|---|
| Road to sale weight | Latest-weigh band per identity (<15, 15-20, 20-25, 25-30, 30-35, 35+ kg) + band movement (up/held/down) | Bands: all identities with a weigh in window (`total_identities`, split matched/unmatched). Movement: identities with ≥2 weighs only (`movement.pair_identities`). |
| Fair fight | Median ADG per shed, per (breed, sex) cohort | Cohort renders only with ≥2 sheds each having ≥3 pair-identities; per-shed `pair_identities` shown on every bar. Unmatched tags fall out (counted in trust panel). |
| Slow-growth watchlist | shed×breed×sex groups vs ~200 g/day ops target (`on_track` / `below_target` / `losing`) | Groups with ≥3 pair-identities only; target disclosed as ops rule of thumb, not a contract. |
| Feed given vs growth | Feed g/head/day vs ADG, kg-feed-per-kg-gained per shed | Always labeled **estimate** (issued ≠ eaten; leftovers unmeasured). Per-animal sheds use median pair ADG; whole-shed sheds use avg-weight delta (`basis` field). Experiment sheds flagged, ratio suppressed. |
| Feed sheet problems | Blocked lines (shed × feed item × days × reason) | Blocked = `quantity_kg IS NULL` only. Authored zeros counted separately in the footnote, never listed as problems. |
| Trust panel | scans matched/unmatched, identities with ≥2 weighs, once-only, whole-shed count, pending verification, rework | The base every other number stands on; all counts scoped to the same filter window. |

## Provenance classes

1. **Live dashboard rollups (untouched):** the KPI strip, park strip, and every existing chart
   keep their current backend semantics. Deliberately preserved.
2. **Growth Director raw aggregates (new):** computed straight from
   `weighing_observations` / `weighing_shed_observations` / `goat_identifiers` / `goats` /
   `feed_direction_*` with the rules above.
3. **Labeled estimates:** everything in Feed given vs growth — directed feed, not consumption.

## Known reconciliation gap (documented, not resolved here)

Raw period sums differ from the live dashboard for the same filter, e.g. 2026-08-11 STG:
raw rows 413 individual + 1,704 whole-shed animal counts = 2,117 kids · 52,561 kg, while the
dashboard shows 1,077 kids · 27,177 kg. Likely dashboard dedupe/latest-per-scope semantics vs
raw repeated campaign rows (also seen per-shed: Yashoda 2 median ADG 343 g raw vs 229 g shown).
**This branch changes neither side.** Growth Director states its own denominators; the live KPI
rollups are preserved as-is. Pinning the summary semantics is a follow-up before anyone compares
the two blocks number-for-number.

## Rollout checklist

- [ ] `make check` green (guards + tests) and `make api-client-check` clean.
- [ ] Backend scoped tests: `cd backend && GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_REQUIRE_DOCKER=1
  go test ./internal/growthdirector/... -count=1` (Docker).
- [ ] Visual regression: existing sections on `/weighing/weights` byte-identical vs the
  2026-08-11 baseline (sheds weighed 49/55, kids 1,077, 27,177 kg, median gain 144 g · 684).
- [ ] STG spot-check trust panel vs known counts (401/413 matched scans; verification backlog
  ~410/413 pending — expect the pending KPI to be nearly total until verification catches up).
- [ ] Backend-owned copy keys reviewed by product; COPY_FALLBACKS kept in sync.
- [ ] Feed prices still missing — cost-per-kg-gained stays out of scope until a price source
  exists (`feed_item_catalog` has no cost column).
- [ ] Follow-up: reconcile live-dashboard rollup vs raw-table semantics (gap above) before
  putting both blocks in one board deck.
