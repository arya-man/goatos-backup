# Feed direction by weight band (ADG Analytics, Weight-wise tab)

Status: implemented on branch `feat/feed-weight-band-table` (2026-09-18), pending review.
Reference (STG-validated prototype the card copies): https://claude.ai/artifact/UPqWtQS17DZ3EA93xLAG6K

## Problem

For the current feed direction, show what feed each pen/cohort is being given and place it
against 5 kg weight bands from the best available weighing evidence, so the CEO and investors can
see which weight group is on which ration.

It is **not** consumption per band. Feed is issued per pen/cohort; the band is observed weight
evidence for the animals in that pen. A per-animal pen with animals in three bands shows its one
feed rollup three times, once per band row, and the tiles never add those rows up.

## Where it lives

- Backend: `GET /growth-director/feed-by-weight-band` (Growth Director module —
  `backend/internal/growthdirector`), `WeighingMonitor`, park-scoped like `/growth-director/weights`.
  It lives outside `backend/internal/weighing` because it joins the feed-direction sheet and the herd
  register to weighing tables, which the weighing isolation guard forbids inside that module.
- Contract: `contracts/openapi/app-api.yaml` (`GrowthDirectorFeedWeightBand*`), page copy and the
  three table contracts (`feed-weight-band`, `feed-weight-band-unmatched`, `feed-weight-band-exits`)
  in `backend/internal/adminui/app/service.go` (`weighing-analytics`).
- Admin-web: `apps/admin-web/features/weighing/weights-analytics.tsx` (`FeedWeightBandSection`),
  `feed-weight-band-table.tsx`, `feed-weight-band-exits-drawer.tsx`, `feed-weight-band-search.tsx`.
  Rendered on `/weighing/analytics?tab=weight`, directly under the Weight-wise bracket chart.

## Data rules (exactly as implemented)

### Feed side

1. **Feed day** = `max(feed_day)` over `feed_direction_issues` with `state IN ('amended','locked')`
   in the parks in scope. Within that day, **one issue per park + workflow**, the latest by
   `coalesce(locked_at, amended_at, issued_at)` (the live unique index already allows only one live
   issue per park/day/workflow; the `DISTINCT ON` is defensive).
2. Rows: `feed_direction_issue_rows` with `quantity_kg > 0`.
3. **Session collapse**: sum `quantity_kg`, max `grams_per_head` per
   (park, shed_label, partition_label, shed_tag, ration_group, experiment_arm, breed, workflow,
   feed_item_label).
4. **Rollup key**: (park, pen, shed_tag, ration_group, experiment_arm, breed, workflow).
   `pen kg/day` = sum over items; `feed given` = items in label order as `<item> <g>g/head` joined by
   ` + `, with the `Dry Masoor ` and `Mesha ` prefixes stripped for display.
5. Display: group from `shed_tag` (F2-* → Fattening; K0–K4, Kid, ICU-Kid → Kid; Buck; Mother /
   Milking → Mother; Pregnant; Non-Pregnant / ICU-Non-Pregnant → Non-Pregnant; Adult; Warmup; an
   unmapped tag renders as itself; a composite tag joins its groups with ` + `). When two rollups in
   one pen would read identically on group, breed, feed type and feed given, the group is suffixed
   with the shed tag: `Kid (ICU-Kid)` vs `Kid (K3)`. Breed: ` x ` → ` cross `.

### Pen key (one rule, three sources)

`penLabelSQL` in `backend/internal/growthdirector/adapters/postgres/feed_weight_band.go`:

- blank / `whole` partition → name alone;
- name already ends with ` <partition>` (`Godel 2 - Part 1` + `Part 1`, `Castro 1` + `1`) → name;
- numeric partition → `name + ' ' + partition` (`Castro` + `1` → `Castro 1`);
- otherwise → `name + ' - ' + partition` (`Godel 2` + `Part 1` → `Godel 2 - Part 1`);
- whitespace collapsed first.

Applied to feed rows (`shed_label` + `partition_label`), campaign sheds
(`weighing_campaign_sheds.display_name` + `partition_label`, which occurs in both the
`'Godel 2' + 'Part 1'` and `'Godel 2 - Part 1' + 'Part 1'`/blank forms) and goat placements
(`locations.name` + `goat_shed_partitions.partition_label`). For goat placements a bare number
(`3`) is read as `Part 3` **only** when that shed has at least one `Part N` partition
(`goatPartitionSQL`); a shed whose partitions are all bare numbers (Castro 1/2/3) keeps them bare.

### Weight side — the General tab's own grain

The weighing side mirrors `backend/internal/weighing/adapters/postgres/shed_weights.go`
(`summary_individual` / `summary_lump`) clause for clause, so the card's totals equal the General
tab's Individual / Lump sum figures for the same filters:

- Scope: non-canceled `weighing_campaign_sheds` of the parks in scope, under the page's Weighing
  filter.
- **Sex / Origin**: the weighing module's own resolvers (`ResolveSexScope`, `ResolveOriginScope`,
  `IntersectScopes`) hand the query an opaque tag list and bucket list; nothing in this read decides
  what "male" means.
- **Same-animal keying**: `ResolveAnimalIdentityMap` over the window plus the 400-day lookback
  (`feedWeightBandLookbackDays`, the same figure as `growthLookbackDays`), so a double-tagged
  animal is one animal.
- **Per animal**: accepted weighs (`verification_status <> 'rejected'`) with `accepted_at` in the
  period plus lookback; an animal **counts only when it has a prior weigh on an earlier date and a
  weigh inside the period**, banded on its latest such weigh, attributed to the pen of that weigh.
  Bands: under 15, 15–20, 20–25, 25–30, 30–35, 35+ (lower-inclusive).
- **Whole pen (lump sum)**: live (`withdrawn_at IS NULL`) accepted shed weighs inside the period,
  under the scope's bucket list; a pen **counts only when weighed on two dates in the period**,
  banded on its latest average with its latest head count. Lump-sum evidence wins over per-animal
  evidence for the same pen.
- **Period**: the page's Period filter (`wt_from`/`wt_to`) bounds the weight evidence only; the feed
  side is always the latest sheet. "Latest" is taken inside the window, so a tag or pen last
  weighed outside it contributes nothing and the pen may become Not shown.

### Gender

From the herd register, never from the shed tag. Per-animal row: sexes of the animals in that
pen × band (tag → newest active `goat_identifiers` row → `goats.sex`); lump-sum row: sexes of the
goats currently placed in that pen (`goats.current_location_id` + `goat_shed_partitions`).
Rendered as `Male`, `Female`, `Mixed 12F·10M`, or blank when no animal resolves.

### Sold / dead

An animal whose goat has `exited_at` set (sold, dead, culled, …) is **shown but excluded from
Wt n and Avg kg by default** (it is not eating today's feed). Each per-animal band row carries
`exited_sold` / `exited_died` (rendered `+N sold` / `+N died` in amber); `animals=all` counts them
in. A pen whose weighed animals have all exited has no band row left and falls to Not shown. The
`exited` list is every goat with `exited_at` inside the period, with its last weigh in the period
(tag, last pen, gender, reason, exit date, last weighed, last band, last kg) and what that pen is
fed today (the pen's first feed rollup). The General-tab reconciliation figure
(`individual_animals_weighed`) still counts exited animals, as that tab does.

### Exclusion

A feed rollup whose pen has no qualifying weighing in the period (or none of the asked sex/origin,
or only exited animals) is **excluded from the banded table, listed under Not shown, and counted**
(`excluded_rollups`). It is never estimated. The count is shown because a fed pen that silently
vanishes reads as a pen nobody feeds.

## Reconciliation — OCI dev DB, as of 2026-09-18

**OCI is not the same data as goatos-stg.** OCI's latest locked feed day is **2026-09-10**
(552 positive rows → 276 items → 137 rollups); STG's is **2026-09-19** (578 → 289 → 140). OCI has
105 exited goats, STG 128. The figures below are OCI's and are not expected to match STG.

Period 03/08/2026 – 15/09/2026, page default Weighing = All, Origin = All.

| Filters (OCI dev DB, as of 2026-09-18) | Sheet rows | Items | Rollups | Matched | Not shown | Band rows | Individual (card / General tab) | Lump sum (card / General tab) | Exited in period |
|---|---|---|---|---|---|---|---|---|---|
| All parks, Sex = Male (page default) | 552 | 276 | 137 | 21 | 116 | 47 | 180 / 180 | 336 / 336 | 52 |
| All parks, Sex = All | 552 | 276 | 137 | 33 | 104 | 94 | 399 / 399 | 336 / 336 | 105 |
| Park = CBE, Sex = All | 364 | 182 | 90 | 15 | 75 | 42 | 211 / 211 | 197 / 197 | 76 |
| Park = CBE, Sex = Male | 364 | 182 | 90 | 9 | 81 | 19 | 91 / 91 | 197 / 197 | 36 |
| All parks, Sex = All, animals = all | 552 | 276 | 137 | 34 | 103 | 110 | 399 / 399 | 336 / 336 | 105 |

**Data event during verification (2026-09-18, ~12:10 UTC):** `goat_identifiers` on the OCI dev
DB was emptied by a process outside this work (3,277 rows deleted; the feature's own reads and
its test harness touch only `goatos_test_*` clones, and none of the test fixtures exist in
`goatos`). After that, with no identifiers to resolve, the same period reads 464 / 336 (all sex),
0 / 336 (male), 265 / 197 (CBE) and 0 / 197 (CBE male) on both the card and the General tab —
still equal, but no longer the figures above. The table above was captured before the wipe.

"Individual" and "Lump sum" are `reconciliation.individual_animals_weighed` /
`lump_sum_animals_weighed` beside `/weighing/shed-weights` `summary.individual_animals_weighed` /
`lump_sum_animals_weighed` for the same query string; they are computed by the same rules and
matched exactly on every combination tried. STG's own General tab reads 183 / 332 for the male
default on 03/08–15/09; the card on STG must be checked against that after deploy, not against OCI.

Tiles on the card dedupe by pen × band × source ("Animals weighed") and by pen × band
("Sold / dead since weighing"); a pen with four feed rows counts its animals once.

## Column glossary

- **Weight source** — `Lump sum` (the Pens table's pill; whole pen on the scale, one row on its
  average) or `Per animal` (tags scanned one by one, one row per bracket).
- **Band** — the bracket label with a six-step bar filled up to it.
- **Wt n** — animals behind the weight: the lump-sum head count, or the bracket's on-farm animal
  count; `+N sold` / `+N died` underneath are weighed animals that have since left (click opens
  the panel for that pen × bracket).
- **Avg kg** — the pen average, or the mean of the bracket's latest weighs.
- **Pen kg/day** — the whole rollup's daily feed, repeated on every bracket row of a per-animal
  pen; never summed across bracket rows.
- **Feed given** — items with grams per head, brand prefixes stripped.
- **Group / Gender / Breed / Feed type** — as above; Feed type is the sheet workflow
  (Normal / Experiment).

## UI

- One card under the Weight-wise chart: feed-day chip, clickable "N fed pens not shown" chip
  (→ Not shown view) and "N sold / dead in period" chip (→ panel), a `Matched | Not shown`
  segmented view, `On farm | Include sold & dead` (Matched only), table-level filters (Feed type,
  Weight source*, Band*, Pen, Group*; * Matched only) with the bar's own Clear, a text search,
  stat tiles, the table, and the shared pager (10/25/50, default 25; lump-sum rows first within
  each park). Table-level state travels in `fb_*` URL params; the page's own Park / Period /
  Weighing / Sex / Origin keep applying on top.
- The sold / dead panel is the app's local drawer (`useLocalOverlaySelection`, `#fb_exit=` hash):
  opened from the chip, the tile or a row note, grouped by pen, searchable, header with count and
  period; Esc / scrim / X / Back close it with filters intact and no route re-run.

## Out of scope / known limitations

- The band is observed weight evidence, not consumption; nothing here allocates feed to a band.
- Unweighed animals appear nowhere: Wt n counts animals with weight evidence, not pen head count.
- `Mixed` gender rows are claimed by neither side of a sex filter (the weighing module's
  agree-or-neither rule); the register, not this read, decides sex.
- Goat placements written as a bare number under a shed that otherwise uses `Part N` are folded;
  a shed that mixes other spellings is not.
- `exited_died` is everything exited that is not sold (dead, culled, lost, transferred, inactive).
- No trend/history: one sheet day against one period.
- Page-level Period defaults come from the weighing SOP window like the rest of the page; when the
  route is opened without a window the page redirects to its canonical dated URL first.
