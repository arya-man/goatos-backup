# COVERAGE MATRIX — every route, tab, overlay, table, chart, control

Rule: a row is CLOSED only when a JUDGE (not a builder) scores it >=8 after looking at ours-vs-reference pairs at 1440+390, dark+light, with hover/motion frames. Builders write to `builder` column; judges write `score`/`judge`.

| Route | Tabs | Overlays | Tables | Charts | Controls | Skeleton | Builder | Score | Judge | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| `/action-center` | 5 | 3 | 2 | 0 | 16 | yes | K (frame/action-center-{d,m}) | 6.5 | M2 r3 | R3: drawer now kit sheet (r16, 24px body) but still prose 'computed from obligation…'; 4 nested tab strips; SOP queue contained 'Verify' + 3 outlined per row ×10; KPI 0 vs 229; kanban '—' cells; 390 minFont 10 |
| `/control-tower` | 2 | 1 | 1 | 0 | 7 | yes | K (served at /; frame/root-{d,m}) | 6.5 | M2 r3 | R3: `/` → /weighing/analytics; scored as that row |
| `/alerts` | 3 | 1 | 1 | 0 | 7 | yes | K (code only: contract absent on OCI stack) | 5 | M2 r3 | R3: still renders Approvals |
| `/actions` | 0 | 0 | 0 | 0 | 0 | NO | — | — | — | |
| `/approvals` | 3 | 2 | 1 | 0 | 6 | yes | K (frame/approvals-d) | 5 | M2 r3 | R3: unchanged; backend internal_error, drawer unreachable |
| `/calendar` | 4 | 3 | 1 | 0 | 10 | yes | K (frame/calendar-{d,m}) | 5.5 | M2 r3 | R3: unchanged: 3 tab components, day strip wraps 3-col at 390 (9.5px), no drives/event drawer |
| `/calendar/drive/[eventId]` | 0 | 1 | 1 | 0 | 4 | yes | — | 4 | M2 r3 | R3: unchanged: PageHeader + 'Back' + red invalid_event_id band; no drive rows |
| `/ceo-ai-admin` | 0 | 0 | 1 | 0 | 1 | yes | K (PageHeader; code only) | 6.5 | M2 r3 | R3: unchanged |
| `/counts/analytics` | 0 | 2 | 0 | 3 | 2 | yes | C: ok 2026-09-19 — monthly columns draw-in 150/450, hover lighten+dim, tip +13/+14, legend top-right; 5 bar lists brand-only + tip; 390 2-col KPIs, sparklines on births/deaths/sold; dark+light; K (frame/counts_analytics-d) | 8 | M1 | R3 (7.5→8): stub sparklines replaced by icon tiles; closed. Nit: 2-month chart floats 4 bars in a wide plot |
| `/counts/breakdown` | 0 | 1 | 2 | 3 | 6 | yes | R (multi-select filters in kit-field anatomy, note + expand hint dropped; shots counts-breakdown-*); C: ok 2026-09-19 — bar lists brand-only; stacked sex hbars now in ChartHover (tip +13/+14) with legend above; grouped loads chart y-axis + tip; dark+light; 390 ok; K (frame/counts_breakdown-d) | 7 | M1 | R3 (7→7): unchanged — 12 KPI tiles incl. zeros; 84px rows; 'Open all' orphan; loads chart tooltip missing on one hit; 9px text at 390 |
| `/counts/herd` | 0 | 4 | 3 | 0 | 14 | yes | R (row hint + pager notes dropped, toolbar label room; shots counts-herd-*) | 7 | M1 | R3 (6.5→7): primary + ⋮ overflow in header. Left: 'Rows per page' floating label still clipped by the toolbar; 30px dead gap between 'Herd' title and toolbar; lowercase `female`; Register-animal dialog ragged label anatomy |
| `/counts/milk-preparation` | 0 | 0 | 1 | 0 | 2 | yes | — | 8 | M1 | R3 (6.5→8): flattened into PageShell sections, notice strips → chips; closed. Nit: filter card holds one field in 1100px |
| `/counts/sops` | 2 | 2 | 0 | 0 | 7 | yes | R (already on kit (shared sops); shots counts-sops-*); K (frame/counts_sops-d) | 7.5 | M1 | R3: unchanged — '4 / 4' orphan; builder prose; two contained buttons in builder header |
| `/feed/sops` | 2 | 2 | 0 | 0 | 7 | yes | — | 7.5 | M2 r3 | R3: renders again ✓ (crash fixed); same as R1: builder prose + 2 contained buttons, Columns dialog no X |
| `/milk/sops` | 2 | 2 | 0 | 0 | 7 | yes | K (frame/milk_sops-d) | 7.5 | M2 r3 | R3: renders again ✓; transient ERR_INCOMPLETE_CHUNKED/CONNECTION_REFUSED in console during capture (dev server), otherwise as /feed/sops |
| `/procurement/sops` | 2 | 2 | 0 | 0 | 7 | yes | R (shared sops page on kit; shots procurement-sops-*) | 7.5 | M1 | R3: same as /counts/sops |
| `/weighing/sops` | 2 | 2 | 0 | 0 | 7 | yes | — | 7.5 | M2 r3 | R3: renders again ✓; as /feed/sops |
| `/feed/analytics` | 3 | 2 | 4 | 6 | 4 | yes | C: ok 2026-09-19 — stacked columns 12 distinct hues, top-only caps, draw-in 150/450, legend top-right, tip +14/+13 (flips up near foot); donut expand+centre swap; per-feed mini charts 11px text at 390; KPI sparklines + day-over-day chips; K (frame/feed_analytics-d) | 7.5 | M2 r3 | R3: unchanged from R2: tooltip ≤6 rows ✓; module+window tabs on one row + in-card segmented; 1 prose subtitle; '30' window badge; 390 KPI titles 3 lines; charts 2 & 4 (donut/area) no tooltip at probe point |
| `/feed/config` | 0 | 2 | 5 | 0 | 6 | yes | K (frame/feed_config-{d,m}) | 5.5 | M2 r3 | R3: unchanged: 'Edit rate' ×10, green 'In force' blocks, non-kit footer, inline UPPERCASE add form, 92px rows; 390 1 fade for 5 scrollers |
| `/feed/direction` | 0 | 1 | 2 | 0 | 5 | yes | K (frame/feed_direction-d) | 6 | M2 r3 | R3: unchanged: card-in-card, KPI sentences, status block; fade ✓ |
| `/feed/packing` | 0 | 1 | 1 | 0 | 4 | yes | K (frame/feed_packing-d) | 6.5 | M2 r3 | R3: unchanged |
| `/goats/[goat_id]` | 0 | 0 | 3 | 0 | 3 | yes | R (shots builders/routes/goats-G-000001-*) | 7 | M1 | R3 (6→7): row-version chip gone, enums capitalised. Left: Identifiers table still clipped at VALID TO with no fade; sticky col un-tinted on hover; Add-identifier fields misaligned (select vs inputs baseline) |
| `/health/analytics` | 1 | 1 | 4 | 3 | 3 | yes | C: ok 2026-09-19 — deaths by month as stacked columns, tip +13/+14, legend top-right; KPI sparklines new cases/deaths; 390 min text 14px; K (frame/health_analytics-{d,m}) | 6.5 | M2 r3 | R3: 'Deaths by month' still shows NO tooltip in my probe (recharts bar hover at bar centre, tip null both bars) and 2 bars in an 1100px plot; tabs pill strip below KPIs; KPI stub sparklines |
| `/health/config` | 0 | 1 | 2 | 0 | 12 | yes | K (frame/health_config-d; Add disease → kit Dialog) | 7.5 | M2 r3 | R3: unchanged: 'Edit' per row, Add-disease top labels / no X, prose footer; dup-key error gone ✓ |
| `/herd-signals` | 4 | 6 | 6 | 2 | 11 | yes | C: 2026-09-19 — no chart element renders on this route (KPI cards only, no series in page data); nothing chart-side beyond KPI count-up; K (frame/herd-signals-{d,m}, drawer) | 5.5 | M2 r3 | R3: tag drawer now kit sheet shell (r16, 24px body) ✓ and history strip has a round axis ✓, but still a hatched red strip + dense mono key/value list with DIRECT/DERIVED chips inline, no movement-history control; live table 94px 3-line mono rows no hover; 6-control header; Gateways prose; Insights no chart; 390 strip 817px, fade ✓ both sides; hydration mismatch now on this route |
| `/leave` | 1 | 0 | 2 | 0 | 3 | yes | K (frame/leave-d) | 6 | M2 r3 | R3: unchanged; duplicate-key console error persists |
| `/operations/audit` | 1 | 1 | 1 | 0 | 7 | yes | K (frame/operations_audit-d) | 5.5 | M2 r3 | R3: unchanged |
| `/operations/dlq` | 1 | 1 | 1 | 0 | 7 | NO | K (frame/operations_dlq-d) | 6 | M2 r3 | R3: unchanged |
| `/people` | 1 | 6 | 5 | 0 | 8 | yes | K (frame/people-d) | 5.5 | M2 r3 | R3: row drawer now kit sheet ✓; rest unchanged (SEARCH STAFF mix, Add person in card, Back/Next, lowercase enums, matrix ×12 Save, Clock-In band, 4 tabs unclickable); hydration '1 Issue' + 409 |
| `/procurement` | 0 | 0 | 0 | 0 | 0 | NO | — | — | — | |
| `/procurement/source-entry` | 2 | 3 | 1 | 0 | 7 | yes | K (frame/procurement_source-entry-{d,m}) | 6.5 | M1 | R3 (6.5→6.5): unchanged — tabs without counts; no header primary; load-id chips wrap; drawer prose paragraph |
| `/procurement/source-entry/loads/[load_id]` | 0 | 2 | 8 | 0 | 9 | yes | R (LEFT: 58-row animals table has no footer/paging; vaccination-evidence form uses native datetime-local; shots procurement-source-entry-loads-*) | 6.5 | M1 | R3: unchanged — dead gap under card title; empty 0-count cards; snake_case chips in timeline; opacity-.45 native select leak |
| `/procurement/feed-purchases` | 0 | 2 | 2 | 0 | 8 | yes | K (frame/procurement_feed-purchases-d) | 8 | M1 | R3: closed; drawer helper prose (3 lines) remains a nit |
| `/procurement/animal-purchases` | 2 | 2 | 2 | 0 | 6 | yes | R (already on kit; dates are ThemedDatePicker; shots procurement-animal-purchases-*) | 7 | M1 | R3 (5.5→7): kit DateRangeField, Loads footer, KPI sentences gone. Left: hydration mismatch back ('1 Issue' overlay); 'All loads' glowing pill; Animals tab strip flush to card edge; broken-image thumbnails; lightbox 1110×1474 radius 0 |
| `/procurement/vendors` | 0 | 1 | 1 | 0 | 10 | yes | K (frame/procurement_vendors-d) | 8 | M1 | R3: closed |
| `/protocol-adherence` | 2 | 4 | 1 | 0 | 5 | yes | K (frame/protocol-adherence-{d,m}) | 6.5 | M2 r3 | R3: row drawer kit sheet ✓; still no eyebrow, 'i' no tooltip, 17 tabs / 2 strips, truncated cells 69px |
| `/routines` | 1 | 1 | 2 | 0 | 4 | yes | K (frame/routines-d) | 6 | M2 r3 | R3: New routine on kit sheet anatomy ✓ (labels, checkbox tiles, footer) but the 21 inputs are still native checkbox/radio + 1 native date; prose 'Pick roles…' remains; page 5 zero KPIs 2+3 |
| `/sales` | 0 | 0 | 0 | 0 | 0 | yes | — | — | — | |
| `/sales/sold` | 1 | 3 | 4 | 5 | 10 | yes | R (prose stripped; shots sales-sold-*); C: ok 2026-09-19 — 3 month-column charts + price-band hbars: brand-only rows, floating tip +14/+14, hover dim, draw-in; dark+light | 8 | M1 | R3 (7.5→8): month charts spaced, labels 11px at 390, KPI sentences gone; closed. Nit: Buyers footer lacks rows-per-page |
| `/sales/farm-value` | 1 | 0 | 0 | 0 | 2 | yes | R (prose stripped; range slider still native; shots sales-farm-value-*) | 7 | M1 | R3 (5.5→7): By-category BarList + KPI sentences gone. Left: BarList rows are flush to both card edges — labels at x=card edge, values touching the right border (needs the 24px card gutter); Apply still sits on the KPI watermark |
| `/sales/loads` | 1 | 1 | 2 | 5 | 3 | yes | R (chip for stock price, in-card scroll, kit footer; shots sales-loads-*); C: ok 2026-09-19 — progress rows now tip+hover; 5 grouped charts: top reserve gone, round y-axis, tip +13/+14, hover dim; equal-height tinted KPIs; 390 ok | 7.5 | M1 | R3 (7→7.5): kit header + footer + sticky y-axis at 390 fixed. Left: footer Rows select sits flush at the card's left edge (no gutter); sticky first column un-tinted on row hover (features/procurement/sales-loads.tsx) |
| `/sales/market-analytics` | 3 | 1 | 1 | 1 | 2 | yes | R (in-card scroll; shots sales-market-analytics-*); C: ok 2026-09-19 — area closes at last point, legend above right, tip follows pointer, 3 container tiers (11px text at 390) | 8 | M1 | R3: closed |
| `/sales/buyer-analytics` | 0 | 0 | 1 | 0 | 3 | yes | R (prose stripped; shots sales-buyer-analytics-*) | 7 | M1 | R3 (7→7): radial card still untitled with 70% empty width; table right-clipped without a footer above the fold |
| `/sales/vendors` | 0 | 1 | 1 | 0 | 1 | yes | R (already on kit; shots sales-vendors-*) | 8 | M1 | R3: closed; Columns/Export orphan row remains a nit |
| `/sales/config` | 0 | 5 | 7 | 0 | 8 | yes | R (prose stripped (survey hints); shots sales-config-*) | 7 | M1 | R3 (7→7): unchanged — native `<input type=time name=call_time>` (features/procurement/market-config-form.tsx); four contained buttons; Record-sale drawer prose blocks |
| `/tasks` | 1 | 1 | 1 | 0 | 5 | yes | R (shots builders/routes/tasks-*) | 7 | M1 | R3 (6.5→7): scope strip + New task stay visible in the error state. Left: `assigned_by_me` still cannot load → drawer unjudged; list-view prose empty state; New task modal close beside title + helper prose |
| `/vaccination` | 1 | 9 | 13 | 0 | 13 | yes | C: 2026-09-19 — no radial/progress chart renders on this route today; duplicate React keys fixed (drive select options, cohort rows); ProgressRow kit now carries tip+hover; K (frame/vaccination-{d,m}) | 5 | M2 r3 | R3: unchanged: 1 fade for 5 overflowing scrollers at 1440, 0 fades at 390 (7 scrollers); section prose; KPI sentence subtitles 9.5px; solid matrix cells; no row hover; hydration '1 Issue' |
| `/vaccination/plan` | 0 | 2 | 3 | 0 | 6 | yes | K (frame/vaccination_plan-d) | 7.5 | M2 r3 | R3: unchanged: version dialog kit ✓; prose line; lowercase chips; no row hover |
| `/vaccination/plan/edit` | 0 | 3 | 1 | 0 | 3 | NO | — | 5.5 | M2 r3 | R3: 'Add a vaccine' STILL `vp-modal` 1440×900 radius 0 (probe R3); 3 prose helper lines; PAGEERROR 'Rendered more hooks' thrown 6× on this route during capture (round3/hooks-error.txt) |
| `/vaccination/live-tracker` | 0 | 1 | 3 | 0 | 5 | yes | K (frame/vaccination_live-tracker-d) | 5.5 | M2 r3 | R3: unchanged: red sub-line, cadence + 2 buttons, ragged filters, 4 prose strips, 6 zero KPIs |
| `/vaccination/execution/sheds/[shedId]` | 2 | 2 | 3 | 0 | 5 | yes | K (PageHeader; code only (no shed id on stack)) | 6 | M2 r3 | R3: unchanged |
| `/verification` | 0 | 0 | 0 | 0 | 0 | yes | — | 6.5 | M2 r3 | R3: alias of /verify |
| `/verify` | 4 | 7 | 4 | 2 | 11 | yes | C: 2026-09-19 — no chart element found on /verify; nothing chart-side to verify; K (frame/verify-{d,m}) | 6.5 | M2 r3 | R3: Video Log / Randomization drawers now kit sheet shell (r16, 24px) ✓ but Randomization keeps 16 contained 'Apply' + repeated prose; video review modal still anchored top-left, native <video> controls; 390: table has 0 fades, header buttons wrap 2+1, minFont 10 |
| `/weighing/weights` | 3 | 3 | 5 | 6 | 8 | yes | C: ok 2026-09-19 — 6 kit BarLists brand-only, rounded tracks, grow-in, hover dim, tip +14/+14; gain-trend area (when data) tip follows pointer | 7 | M2 r3 | R3: chart 2 now has a tooltip but it lands 85px from the pointer (clamped at card edge); rest unchanged (KPI orphan, in-card toggles, prose note, 'male') |
| `/weighing/analytics` | 1 | 4 | 4 | 4 | 6 | yes | C: ok 2026-09-19 — weekly gain area tip +14/+14; breed/birth/pen/time tabs on kit BarList (series legend top, negative = danger); acceptance pairs 12/13/14 in builders/charts/acceptance; K (frame/weighing_analytics_tab_*) | 6.5 | M2 r3 | R3: PAGEERROR 'Rendered more hooks than during the previous render' STILL thrown — 8 occurrences across the R3 capture (round3/hooks-error.txt, 04:33–05:07) + '1 Issue' overlay on every tab; Pen-wise BarList hover still no tooltip at probe point; 'Weekly growth' prose empty state; Pens filter bare 'Apply'; 390 5 stacked filters |
| `/work-board` | 2 | 5 | 1 | 0 | 15 | yes | R (shots builders/routes/work-board-*) | 7 | M2 r3 | R3: modal now centred kit dialog 960 r16 ✓; still 3 pill buttons per subtask ×10, no primary, card hover no lift, 390 minFont 9 |
| `/workflows` | 1 | 2 | 2 | 1 | 5 | yes | C: 2026-09-19 — no chart element found on /workflows; K (frame/workflows-{d,m}) | 6 | M2 r3 | R3: unchanged: Filters dialog prose + contained 'Open Action Center'; no eyebrow; minFont 10 |
| `/workflows/[row_id]` | 0 | 1 | 1 | 2 | 2 | yes | K (PageHeader; code only) | 3.5 | M2 r3 | R3: row link still 'invalid_row_id' |
| `(shell)` | 2 | 5 | 0 | 0 | 10 | yes | K (frame/shell-{rail,avatar-menu,light}-d.png, shell-mobile-nav-m.png, hardnav-verify-{150,400,900}ms.png) | — | — | |
| `/login` | 0 | 0 | 0 | 0 | 2 | NO | K (stack auto-signs in (bearer); not reachable) | — | — | |
| `/auth/action` | 0 | 0 | 0 | 0 | 1 | NO | K (not reachable on stack) | — | — | |
| `/kit-preview` | 0 | 0 | 0 | 0 | 1 | NO | — | — | — | |
| `/tasks-preview` | 0 | 0 | 0 | 0 | 1 | NO | — | — | — | |
| `/` | ? | ? | ? | ? | ? | ? | K (frame/root-{d,m}) | 6.5 | M2 r3 | R3: → /weighing/analytics |
| `/configuration/items` | ? | ? | ? | ? | ? | ? | — | 7.5 | M1 | R3 (5.5→7.5): footer '1–3 of 3', State select value, header band tint. Left: page title is eyebrow+h1 only (no kit PageHeader/breadcrumb); sidebar count 2 vs 3; 390 section nav fills a screen |
| `/counts/mortality` | ? | ? | ? | ? | ? | ? | R (shots builders/routes/counts-mortality-*); C: ok 2026-09-19 — stacked monthly columns (Kids/Adults) legend in header, tip +13/+14, draw-in; 390 ok | 8 | M1 | R3 (7.5→8): KPI prose gone; closed. Nit: row hover tint lingers |
| `/sales/farm-born` | ? | ? | ? | ? | ? | ? | R (shots builders/routes/sales-farm-born-*) | 8 | M1 | R3: closed |

## Shell rows
| Element | Builder | Score | Judge | Notes |
|---|---|---|---|---|
| Sidebar: every group expanded | — | 6 | M2 r3 | R3: unchanged: no leaf hover state; 2014px list scrolls eyebrows away |
| Sidebar: collapsed rail | K (frame/shell-rail-d.png) | 7.5 | M2 r3 | R3: unchanged ✓ |
| Sidebar: active leaf + auto-expand + scroll-into-view | K (frame/operations_audit-d.png, weighing shots) | 7 | M2 r3 | R3: unchanged; /calendar no active leaf |
| Topbar: park selector | — | 7.5 | M2 r3 | R3: ✓ |
| Topbar: theme toggle (dark/light) | — | 8 | M2 r3 | R3: ✓ |
| Topbar: bell + badge | — | 7.5 | M2 r3 | R3: ✓ |
| Notifications panel: All/Unread/Archived, rows, actions, load-more, empty, skeleton | — | 7.5 | M2 r3 | R3: skeleton rows now shown at open (4 sk at 40ms) ✓; footer shortened to 'Push blocked in browser' ✓; still no Archived tab, empty state unreachable |
| Topbar: avatar menu | — | 7.5 | M2 r3 | R3: ✓ kit popover; 'Sign out' red |
| 404 page | — | 7 | M2 r3 | R3: renders again ✓ (in-shell, eyebrow, crumb, oversized empty card) |
| Error boundary | — | 5 | M2 r3 | R3: not reachable again; bad ids still fall to per-route red bands; /goats/NOPE internal_error no retry |
| Global loading | — | 8 | M2 r3 | R3: ✓ hard-nav; soft-nav still no pending state |
| Login | — | — | M2 r3 | R3: not reachable |
| CEO chat FAB | — | 5 | M2 r3 | R3: unchanged: click opens nothing detectable |
| Mobile drawer nav (390) | — | 6.5 | M2 r3 | R3: unchanged: 320px + close X, still no scrim element |