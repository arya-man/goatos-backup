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

**Latest sheet is per park and per workflow.** `latest_issue` is `DISTINCT ON (park_id, workflow)`
over locked/amended issues ordered `feed_day DESC, coalesce(locked_at, amended_at, issued_at)
DESC` — no global newest day. An all-parks read therefore takes CBE's latest sheet AND CPT's
latest sheet even when one is a day older (the first cut filtered every selected park to one
`max(feed_day)` and dropped the park with the older sheet; Codex P1 on PR #304, pinned by the
integration fixture where park B's latest sheet is one day older than park A's).
`reconciliation.feed_sheets` lists the sheet read per park and workflow; `feed_day` is the newest
of them. The chip reads "Feed plan 19/09/2026" when every plan is the same day, otherwise
"Feed plans · CBE 19/09/2026 · CPT 18/09/2026" (with the workflow named when a park's normal
and experiment sheets differ).

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
  banded on its latest average with its latest head count. The two dates and the latest weigh are
  taken on the **folded pen key across every bucket spelling** of that pen (`Godel 2`+`Part 1` and
  `Godel 2 - Part 1` are one pen) — the one place this read is wider than `shed_weights.go`, which
  pairs per campaign-shed bucket; on OCI/STG data the lump total still equals the General tab
  (332). Lump-sum evidence wins over per-animal evidence for the same pen.
- **Period**: the page's Period filter (`wt_from`/`wt_to`) bounds the weight evidence only; the feed
  side is always the latest sheet. "Latest" is taken inside the window, so a tag or pen last
  weighed outside it contributes nothing and the pen may become Not shown.

### Gender

From the herd register, never from the shed tag. Per-animal row: sexes of the animals in that
pen × band (tag → newest active `goat_identifiers` row → `goats.sex`); lump-sum row: sexes of the
goats currently placed in that pen (`goats.current_location_id` + `goat_shed_partitions`).
Rendered as `Male`, `Female`, `Mixed 12F·10M`, or blank when no animal resolves.

### Exited animals (sold / died / other)

**Exited = `goats.exited_at` is set**, whatever the reason: sold, died, or any other removal from
the register (an `inactive` record, a transfer, a loss, a cull, a blank reason). The rule is
`domain.FeedExitBucket(lifecycle_status, exit_reason)`, mirrored by the two flags in
`feedWeightBandSQL`'s `animal_sex` CTE:

| Bucket | Register words (`lifecycle_status` / `exit_reason`, case-folded) |
|---|---|
| `sold` | either is `sold` |
| `died` | either is `dead` or `died` (and not sold) |
| `other` | everything else with an `exited_at` |

**The exit population is Herd Analytics' exits**: every goat with `exited_at` inside the period
(`merged_into_goat_id IS NULL`), under the page's park, **sex (`goats.sex`) and origin
(`procurement_load_goats` membership = purchased, absence = farm born, per animal)** filters —
never narrowed to animals with weighing evidence. On the OCI clone for 03/08–15/09 that is
**65 under Sex = Male (56 sold · 3 died · 6 other)** and **120 all sex (104 · 3 · 13)**, the same
figures Herd Analytics' Sold / Deaths report as its first two buckets; `other` is the register's
`inactive` records with no reason. Each exit carries `weighed_in_period`: **55 of the 65 male /
99 of the 120 all-sex exits have a weigh inside the period** and only those can appear on a band
row; the rest are listed with their current placement as the pen and no band or kg
(`reconciliation.exited_weighed + exited_not_weighed = exited_animals`). The band rows' own
`+N sold / died / other` notes and the Include-exited head counts are band evidence and count
weighed animals only. An exited animal is **shown but excluded from Wt n and Avg kg by default** (it is not
eating today's feed). Every per-animal band row carries **both head-count variants in one
payload** — `weight_animals` / `average_weight_kg` / `gender` (on farm) and the `*_all` twins
(every weighed animal) — plus `exited_animals = exited_sold + exited_died + exited_other`, so the
card's `On farm | Include exited` toggle is a client-side flip with no second read (rendered
`+2 sold` / `+1 died` / `+1 other` in amber on farm, `incl. 2 sold` when included). A band row
whose animals have all exited has `weight_animals = 0`: under On farm the card files that pen
under Not shown; under Include exited it is a Matched row. The `exited` list is every goat with
`exited_at` inside the period (`bucket` beside the stored `reason` / `lifecycle_status`), with its
last weigh in the period and what that pen is fed today; `reconciliation.exited_sold / _died /
_other` are the same three buckets over that list. The General-tab reconciliation figure
(`individual_animals_weighed`) still counts exited animals, as that tab does.

### Exclusion

A feed rollup whose pen has no qualifying weighing in the period (or none of the asked sex/origin,
or only exited animals) is **excluded from the banded table, listed under Not shown, and counted**
(`excluded_rollups`). It is never estimated. The count is shown because a fed pen that silently
vanishes reads as a pen nobody feeds.

## Reconciliation — OCI dev DB, as of 2026-09-18 (post-resync)

**OCI is not the same data as goatos-stg.** OCI was re-synced from goatos-stg at ~17:50 IST on
2026-09-18; at that moment its latest locked feed day is **2026-09-19** (578 positive rows → 289
items → 140 rollups) and its General tab reads 183 / 332 for the male default, the STG figures
the maintainer quoted. The two databases drift again from the next STG write; the card on STG
must be checked against STG's own General tab after deploy.

Period 03/08/2026 – 15/09/2026, page default Weighing = All, Origin = All. Matched / Not shown /
Band rows are given as `On farm / Include exited`; Individual and Lump sum are the card's
`reconciliation.*_animals_weighed` beside `/weighing/shed-weights` `summary.*` for the same query.

| Filters (OCI dev DB, as of 2026-09-18) | Sheet rows | Items | Rollups | Matched | Not shown | Band rows | Individual (card / General tab) | Lump sum (card / General tab) | Exited in period |
|---|---|---|---|---|---|---|---|---|---|
| All parks, Sex = Male (page default) | 578 | 289 | 140 | 20 / 22 | 120 / 118 | 48 / 59 | 183 / 183 | 332 / 332 | 65 = 56 sold + 3 died + 6 other (55 weighed) |
| All parks, Sex = All | 578 | 289 | 140 | 33 / 36 | 107 / 104 | 94 / 111 | 413 / 413 | 332 / 332 | 120 = 104 sold + 3 died + 13 other (99 weighed) |
| Park = CBE, Sex = All | 374 | 187 | 89 | 15 / 18 | 74 / 71 | 41 / 53 | 218 / 218 | 193 / 193 | 91 = 88 sold + 2 died + 1 other (83 weighed) |
| Park = CBE, Sex = Male | 374 | 187 | 89 | 8 / 10 | 81 / 79 | 20 / 26 | 94 / 94 | 193 / 193 | 43 = 40 sold + 2 died + 1 other (39 weighed) |

The exit list is the register's exits under the page's filters (a male read lists the 65 male
exits, weighed or not), reconciling to Herd Analytics; 55 of those 65 (99 of the all-sex 120)
carry a weigh in the period.

Worked example, CBE Yashoda 3 · 15–20 kg, Sex = Male: On farm `Wt n 9, +2 sold, Avg 17.1`;
Include exited `Wt n 11 incl. 2 sold, Avg 17.3`. Seven of the 48 rows on the male default change
between the two readings, and a 30–35 kg Yashoda 3 row (3 animals, all sold) exists only under
Include exited.

**CBE Godel 2 - Part 3 reads Per animal under Sex = Male, not Lump sum, by the General tab's own
rule.** STG holds one lump-sum weigh for that pen in the period (21.05 kg average, pending
verification, n = 2). Pending IS counted — `verification_status <> 'rejected'` admits pending,
rework and verified alike — but a whole-pen weigh contributes only when the pen was weighed on
**two dates** in the period (`summary_lump`'s pairing), and a single date is not a pair. The
pen's scanned animals do pair, so the per-animal rows stand. The rule is not changed here.

The "Animals weighed" tile dedupes by pen × band × source (a pen with four feed rows counts its
animals once); the "Exited in period" tile is the reconciliation figure with its weighed /
not-weighed split.

### Serving shape and measured latency

One read per top-bar change: `GetFeedWeightBandSource` is wrapped in the repository's 30 s burst
cache + single flight (`growthDirectorReadKey("feed_weight_band", …)`, the `GetGrowthDirectorWeights`
pattern), the two scope resolvers and the identity map run concurrently, then the sheet query and
the exits query run concurrently — at most three pool queries in flight per request. Everything on
the card (view, Animals, the five filters, search, pager) is client state over that one payload;
opening the exited panel issues no request.

Measured on the laptop against OCI through the SSH tunnel (n = 30 warm after 5 warm-ups; cold =
20 requests with distinct cache keys), before → after:

| Read | Before p50 / p95 (perf judge, uncached) | After cold p50 / p95 | After warm p50 / p95 |
|---|---|---|---|
| default (all parks, sex = male) | 585 / 759 ms | 416 / 667 ms | 51 / 139 ms |
| park = CBE, sex = male | 371 / 414 ms | 240 / 316 ms | 25 / 128 ms |
| sex = all | 392 / 442 ms | 291 / 344 ms | 67 / 287 ms |
| animals = all (now the same read as default) | 585 / 854 ms | — (same key as default) | 63 / 146 ms |

Cold sex = male stays above 500 ms at p95 because `ResolveSexScope` (the weighing module's own
resolver, 195 ms execution on OCI) precedes the tag-filtered sheet query; that resolver is shared
with the General tab and is not changed here. Browser Animals toggle, click → rows painted:
**753 ms with an RSC round trip before → 20–25 ms and 0 requests after**.

## Column glossary

- **Weight source** — `Lump sum` (the Pens table's pill; whole pen on the scale, one row on its
  average) or `Per animal` (tags scanned one by one, one row per bracket).
- **Band** — the bracket label with a six-step bar filled up to it.
- **Wt n** — animals behind the weight: the lump-sum head count, or the bracket's on-farm animal
  count; `+N sold` / `+N died` / `+N other` underneath are weighed animals that have since exited
  (click opens the panel for that pen × bracket).
- **Avg kg** — the pen average, or the mean of the bracket's latest weighs.
- **Pen kg/day** — the whole rollup's daily feed, repeated on every bracket row of a per-animal
  pen; never summed across bracket rows.
- **Feed given** — items with grams per head, brand prefixes stripped.
- **Group / Gender / Breed / Feed type** — as above; Feed type is the sheet workflow
  (Normal / Experiment).

## UI

- One card under the Weight-wise chart: one-line caption (the rules live in the ⓘ, which also
  defines "Feed plan" = the locked daily feed-direction record in GoATOS for that date, not a
  spreadsheet), feed-plan chip, clickable "N fed pens not shown" chip
  (→ Not shown view) and "N exited · N weighed" chip (→ panel; the sold / died / other split and
  the not-weighed count are its tooltip and the panel header), a
  `Matched | Not shown` segmented view, `On farm | Include exited` (Matched only), table-level filters (Feed type,
  Weight source*, Band*, Pen, Group*; * Matched only) with the bar's own Clear, a text search,
  at most eight stat tiles, the table, and the shared pager (10/25/50, default 25; lump-sum rows first within
  each park). Table-level state travels in `fb_*` URL params; the page's own Park / Period /
  Weighing / Sex / Origin keep applying on top.
- The exited panel is the app's local drawer (`useLocalOverlaySelection`, `#fb_exit=` hash):
  opened from the chip, the tile or a row note, grouped by pen, searchable, header with count and
  period, Reason column = bucket pill beside the stored register text, weighed animals grouped by
  pen and the not-weighed ones last under "No weighing in period" with placement as pen and — for
  band / kg; Esc / scrim / X / Back close it with filters intact and no route re-run.

## Out of scope / known limitations

- The band is observed weight evidence, not consumption; nothing here allocates feed to a band.
- Unweighed animals appear nowhere: Wt n counts animals with weight evidence, not pen head count.
- `Mixed` gender rows are claimed by neither side of a sex filter (the weighing module's
  agree-or-neither rule); the register, not this read, decides sex.
- Goat placements written as a bare number under a shed that otherwise uses `Part N` are folded;
  a shed that mixes other spellings is not.
- `other` exits (inactive, transferred, lost, culled, blank reason) are reported as their own
  bucket, never folded into died; the register's wording decides the bucket, not this read.
- No trend/history: one sheet day against one period.
- Page-level Period defaults come from the weighing SOP window like the rest of the page; when the
  route is opened without a window the page redirects to its canonical dated URL first.
