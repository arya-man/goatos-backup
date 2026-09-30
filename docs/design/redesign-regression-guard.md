# Admin-web redesign regression guard

Scope: UI bug fixes in admin-web and the mobile webviews from 2026-07-25 to 2026-09-25, on `origin/main` and on PR #294 (`design/minimal-redesign-preview`). Each item is an invariant the redesign must keep.
Paths are relative to `apps/admin-web/`. "theme" means `app/mesha-theme.css`, "frame" means `app/frame.css`, and "minimal" means `app/minimal-theme.css`.

## 1. Charts: animation, hover, tooltips, labels

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| be0d9cfe3 | PR | Tooltips rendered off-screen under the page-enter transform. Hover had no feedback. | components/chart-hover.tsx, charts-premium.css, kit/charts.tsx, grouped-columns.tsx, kit/bar-list.tsx, month-columns.tsx | Tooltips are portaled to `document.body` and follow the pointer (+14px, edge flip). They use a 150ms fade/scale and never re-mount per hover move. Draw-in starts after the skeleton swap. reduced-motion disables motion. |
| df8333873 | PR | Below-fold charts ran their draw-in off-screen. Charts did not reset on a scope change. | kit/use-in-view-once.ts, kit/charts.tsx, svg-series.tsx, kit/sparkline.tsx, kit/radial-stat.tsx, chart-hover.tsx | Draw-in fires once, on first visibility, and never replays on hover or re-render. Charts are keyed by scope, so a park or period change re-animates exactly once. |
| 942e6c055 | PR | Charts that straddled the fold never drew in at load. | kit/use-in-view-once.ts, kit/charts.tsx, features/feed/feed-shed-feed-charts.tsx, features/procurement/sales-farm-value.tsx | A chart that is partly visible at load animates immediately. A chart is never left at 0 height or opacity. |
| 016b18d22 | PR | Hover missed on thin bars, donut wedges, area days and bar-list headings. The tooltip flip anchor was off. | chart-hover.tsx, svg-series.tsx, kit/bar-list.tsx, kit/charts.tsx, features/weighing/grouped-bars.tsx | Every datum has a full-plot hit area (a full column or day slot, not just the painted pixels). The flip uses the exact pointer anchor. |
| defa7b73c | PR | The stacked-column tooltip was huge and covered the chart. | chart-hover.tsx, minimal | The stacked tooltip sits beside the pointer, at most 220px wide and at most 6 rows, with a "+N" fold. |
| 3c8dd02ad | PR | Health deaths and weighing bar lists had no tooltip, so their hover area was dead. Ticks were under 11px. | charts-premium.css, features/herd-signals/herd-signals-history-chart.tsx | Every bar list shows a tooltip. Tick text floor is 11px (mobile) and 12px (desktop). The history-strip axis uses round ticks. |
| 93bea806c | PR | Multi-colour single-series bars. Duplicate React keys on /vaccination. No tooltips on breakdown or loads bars. | kit/progress-row.tsx, charts-premium.css, features/preventive-care-vaccination/command-board-view.tsx | A single-series bar list uses one brand colour. Chart item keys are unique, which prevents re-mount flicker. |
| 6ec6739bf | PR | Month column labels collided. The y-axis scrolled away at 390px. Sparklines were fabricated. | charts-premium.css, kit/charts.tsx, features/counts/herd-analytics.tsx | The y-axis stays sticky at 390px. Month labels never overlap. A sparkline is drawn only from a real series. |
| 88c3a47ce | PR | The SvgBars narrow tier rendered 9px labels on phones and ignored `textScale`. | components/svg-bars.tsx, theme | Phone chart text is at least 11 CSS px. The narrow tier honours the caller's `textScale`. |
| 0d844b3a3 | main | Charts used the native `title` for hover (unstyled and slow). | components/grouped-columns.tsx, theme, features/procurement/sales-loads.tsx | Columns show a styled hover card (header plus one swatch row per series), never a native `title`. |
| e6a428019 | main | The vaccination weekly hover tooltip was removed by request. | features/preventive-care-vaccination/command-board-view.tsx, theme | Do not reintroduce the weekly hover tooltip on the command board. |
| 229973f10 | main | A card-header "i" tooltip was cut off by the card's `overflow:hidden`. | components/ui-primitives.tsx, theme, features/health/health-register-sheet.tsx | An info popover at the right edge opens leftward, or is portaled. No popover is clipped by `.card`. |
| 38924a13e | main | A measured zero printed no figure, and column figures disappeared on phones. | components/month-columns.tsx, grouped-columns.tsx, theme | Every column shows its value, including 0, at every width. |
| 1cc68799a | main | The label on the tallest load-chart bar was cut in half or missing on phones. | theme | The chart area reserves headroom so the value label on a 100% bar is fully visible. |
| 9f2642cfd | main | Grouped charts drew empty slots, and loadwise labels collided in the webview. | grouped-columns.tsx, svg-column-bars.tsx, svg-series.tsx, theme | Empty series slots are not rendered. Webview chart labels do not collide. |
| 5a3603b4f | main | Bar rows read as one solid block. | components/svg-bars.tsx, theme | The bar gap is 10 viewBox units (half a bar), set once for all bar charts. |
| 73ca7977e | main | Stacked columns clipped 4+ digit y ticks, first and last dates were missing, and items were folded into "Other". | svg-series.tsx, features/feed/feed-analytics.tsx | Left padding sizes to the widest tick. First and last date labels are shown. Every feed item is drawn. |
| a09e238a1, c15ce6300, 9b3732878 | main | The spend-share pie showed with no data, its legend overflowed on narrow cards, and the money was far from the label. The path was mutated. | svg-series.tsx, features/feed/feed-analytics.tsx | The pie is hidden when no slice qualifies. The legend label wraps while the value stays beside it. Path construction is immutable. |
| 7d950440d, dcd1677c3 | main | The current month was dropped, and sparse month charts stretched. | month-columns.tsx, theme, features/procurement/sales*.ts(x) | The current month is always present. Sparse month charts stay compact. |
| a4b91e1d3, c8e11e56f | main | Sales SVG labels scaled down to about 5px in half-width cards and had no month labels. They were unreadable on mobile. | hbar-list.tsx, month-columns.tsx, theme, features/procurement/sales.tsx | Sales charts use fixed-size HTML labels, not viewBox-scaled text. Month and value labels are always shown. |
| a4bc494b2, 47c9a4069, d49a9b1a2, 0235a573a, a35063e4f | main | Weights charts shrank inside half-width cards, had no inset, clipped names to 4px and overlapped shed labels. Row order changed between metrics. | features/weighing/weight-bars.tsx, weights.tsx, growth-director.tsx, theme | Weight charts keep legible text two-up. Cards have an inner inset. The label column has a real floor, and labels never overlap. Row order is stable across a metric toggle. |
| e311ef24c, 1a9ec9452 | main | FCR wide bar labels collapsed to "C.." on phones. | theme, scripts/lib/feature-assertions.mjs | At 900px or narrower, a bar row stacks the label, tag and value above a full-width bar. |
| c54063b74 | main | Grouped weighing values broke on mobile. | theme | Grouped weighing values remain readable at 390px. |
| 4c83e8fc8, bc874d4b5, 4a2699d99, f2a756df8, 367fe41d5 | main | The herd-signals drawer chart rescaled or jumped, its bar heights were lost, it crashed on tag select and cancelled itself, and it did not draw at 7d or 30d. | features/herd-signals/herd-signals-drawer.tsx, herd-signals-history-chart.tsx, herd-signals-history-fullscreen.tsx, theme | The drawer chart scale and bar heights stay stable across refresh and tag change. The chart draws at every range. |
| 190e3fd2a | main | Ask Mesha chart dots and ticks were wrong, and the chart was not full width. | features/ceo-ai/ceo-ai-chart.tsx, ceo-ai-chart-geometry.ts | Ask Mesha charts span full width with correct dots and ticks at 390px and 1440px. |

## 2. Tabs, skeletons and render flicker

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| 63df96c9b | PR | The tab indicator and panel flickered, and the height jumped on a tab switch. | kit/animated-tabs.tsx, frame, kit/bar-list.tsx | Tabs have one measured indicator (transform plus ResizeObserver) and a panel crossfade with reserved height. There is no flicker and no height jump. |
| 7e663df70, 99590f73b, 100f39226 | PR | `/` showed two skeleton phases (a double redirect). Content was blanked with `display:none`. Tab skeletons were the wrong shape. | app/(admin)/layout.tsx, app/(admin)/page.tsx, weighing/analytics/page.tsx, frame, */loading.tsx | Exactly one skeleton phase on a direct load or navigation. Tab skeletons match the real column tracks. Empty data renders a kit EmptyState, never a blank card. |
| 528613881, f8ccf1229 | PR | Alias routes flashed a skeleton. | proxy.ts, lib/route-aliases.ts, components/route-skeleton.tsx | Aliases (such as /alerts) redirect in the proxy with no skeleton. Closed `<details>` are not blank. |
| 03a6ef3b3, 4551e4f93 | PR | The Weekly growth card was empty on the General tab, and the redesign had added it there. | weighing analytics | Weekly growth lives on the Time-wise tab only, as on main. |
| 622787c2e | main | The notification bell dispatched a Server Action inside a state updater, which caused a render-time router update. | features/notifications/notification-bell.tsx, theme | State updaters stay pure. Panel placement is measured in a layout effect. |
| 37940f73f | PR | Bell Server Action POSTs crashed the router on redirecting pages. | notification bell route handler | The bell reads through a GET route handler, not a Server Action. |
| 5546bb720, e40a8dd03 (reverted in 9b2dd2b45) | main | The verifier drawer stuck at opacity 0 and inert, because opening depended on one animation frame. | features/verification-review/verification-review-drawer.tsx | Drawer open state never depends on a single rAF. A click always shows the panel. |
| 106694bd0 | main | A poller `router.refresh()` closed the open tag drawer. | features/herd-signals/herd-signals-poller.tsx | Background refreshes never close an open drawer. |
| 3287e9466, f6060a2c5 | main | Stale park scope showed in the shell chrome, and the picked period was lost on a park change. | components/mesha-shell.tsx, lib/scope.ts, components/worklist-filters.tsx | Shell scope chrome reflects the current park. A picked period survives a park change. |

## 3. Tables

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| 53d8d4746, be5436080 | main | Tables overflowed the viewport on mobile. | theme, components/data-table.tsx, scripts/smoke-visual-live.mjs | Every wide table scrolls inside its own wrapper, and the page body never scrolls horizontally. |
| 0260c88fe | main | The health-config table had the wrong scroll owner. Checkboxes and breadcrumbs were under 40px. | theme, features/health/health-config.tsx, features/people/notification-matrix.tsx | Each table has one scroll owner. Tap targets are at least 40px. |
| d80b1b84b, ba1fdc3cf | main | Global `.celllink{overflow-wrap:anywhere}` split codes character by character (CBE to C/B/E, CPT to CP/T). | theme, features/procurement/feed-purchases.tsx | Short codes, load numbers and rates never break mid-token. |
| 2998f101e | main | People columns painted over the next cell. | theme, features/leadership-tasks/leadership-tasks-page.tsx | People columns have a 170px floor and wrap. Cell text never paints over a neighbour. |
| 1d9decfc3 | main | The Days counter was hidden behind horizontal scroll. | leadership-tasks-page.tsx, new-task-modal.tsx, theme | Days is the 2nd column on desktop and sits under the title on phones. |
| 4350f9eee | main | An empty "Select a task" rail took 54% of the width and hid columns. | theme, leadership-tasks-page.tsx | The list uses the full width. Horizontal scroll is signalled. |
| be4b1a92a | main | Alerts Detail squeezed to one word per line, and arrows were 32px. | theme | Text columns have a min-width, so the table scrolls instead. Arrows are at least 40px. |
| 9fe59652d | main | The inline tag editor popup was clipped by the table's scroll container. | features/counts/inline-cell-editor.tsx, theme | Popups inside a scroll table escape the clip through fixed or portal positioning. |
| f7d4c4034 | main | The sales market table overflowed. | theme, features/procurement/sales.tsx | The market table stays inside its card. |
| 1cd909beb, fae12c36c, 08d0051c7 | main | Feed-band breed and feed were truncated, and the exits drawer clipped a column at 1280. | features/weighing/feed-weight-band-*.tsx, theme | Breed and feed text is readable. Mobile gets cards. The exits drawer fits every column at 1280. |
| cc29bb1f3 | PR | The protocol-adherence Gap sentence was cut inside a nowrap `.tag` chip. | frame, theme | Flex children in cells have `min-width:0`. A chip holding a sentence may wrap. |
| 06a923ffa, 82df2b4d5, e7046979c | PR | Vaccination plan, procurement and verify tables were unusable on mobile. | theme, features/procurement/procurement-minimal.css | These tables render as cards at phone width. |
| adf3b8cae | main | The Diagnosis explainer and template sat inside the table. | features/health/health-register*.tsx | Explainer and template copy stay outside the table body. |

## 4. Sidebar, shell and navigation

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| 7029134a8 | main | Sidebar labels used 14, 11 and 13px, so the rail read as three lists. | tools/agent-hooks/check-sidebar-typography.mjs | Every sidebar navigation label uses one size, the size of "Verify" (guarded by check-sidebar-typography). |
| 63df96c9b | PR | Sidebar grouping and active-state bugs. | frame, shell | Section eyebrows are shown. The active group auto-expands and the active leaf scrolls into view. |
| 7e663df70 | PR | The WebView sidebar scrolled away. | frame, components/admin-shell.tsx | The sidebar is sticky inside the layout scroller in the WebView. |
| bc8864617, 99cc89fac | main | The page showed through or behind the mobile menu, and responsive sidebars broke. | theme | When the mobile menu is open, the page behind it is hidden and inert. Sidebars stay contained at every breakpoint. |
| f86c54270 | main | Sidebar navigation had tail latency. | features/people/people-page.tsx | Sidebar navigation must not block on slow reads. |

## 5. Layout overlap, clipping, drawers and overlays

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| a96d5318e | PR | Verify drawers anchored to the transformed PageEnter instead of the viewport, their z-index was trapped, and footers overflowed. | features/verification-review/{analytics,randomization,video-log}-panel.tsx, theme | Every fixed overlay goes through BodyPortal, never under a transformed ancestor. Drawer footers wrap. |
| 51a447cdb | main | `backdrop-filter` on `.lt-fbar` made the phone sheet anchor to the bar and grow off-screen. | theme | No `backdrop-filter`, `transform` or `filter` ancestor over a `position:fixed` sheet. Sheets anchor to the viewport. |
| 8f452925a, ac8157375 | main | The filter sheet turned see-through while a filter loaded (`.wfbusy`). | theme | Busy dimming never applies to a sheet-hosting bar. The sheet stays opaque. |
| 03684aee5 | main | Expand stacked the full-screen view under the drawer, so Close was unclickable. | components/local-overlay-link.tsx | Opening a full-screen view replaces the drawer. Close always receives pointer events. |
| 7eb90d4cd | main | The drawer rendered as a sibling of `.dscrim` and stayed off-canvas. | command-board-view.tsx | A `.drawer` is always a descendant of its `.dscrim`. |
| 1844535f5 | main | Modal sections shrank to nothing inside the flex scroller. | theme | Sections inside a scrolling flex column are `flex-shrink:0`. |
| 5e07259aa, 75f47bed3 | main | Task detail took 54% of the board, and the drawer header scrolled away. | leadership-tasks-page.tsx, task-detail-drawer.tsx, theme | Task detail is a drawer over the board. The drawer header stays sticky on an opaque background. |
| 08a060e72 | main | `.card{overflow:hidden}` beat the properties panel's scroll. | theme | The SOP studio properties panel scrolls inside itself. |
| 549fb919a | main | Flow branches overlapped the spine. | features/sops/flow-layout.ts, followup-flow.tsx | Branches sit beside the spine, with labels at the fork. |
| 0cb82f13d | main | A long unbroken title pushed the dialog rail off-screen. | theme, features/work-board/work-board-subtasks.tsx | Long unbroken strings wrap (`overflow-wrap:anywhere` on titles). The grid uses `minmax(0,1fr)`. |
| c75b8a003 | main | The pen-visit blurb ran off the editor. | theme, features/people/person-access-modal.tsx | Sentences in nowrap strips take their own row and wrap. |
| 69f9b7d2e | main | Checkbox rows were stretched by the text-field width rule. | theme | Checkboxes keep their own size. |
| dc49a6b18 | main | The vaccination plan editor overflowed by 5px. | theme, features/vaccination-plan/plan-editor.tsx | Zero horizontal overflow. |
| 3507f16ee | main | Vaccination next-action text overflowed. | theme, features/vaccination-execution/execution-board.tsx | Next-action text wraps. |
| 3ac7c258b | PR | Notification bodies were cut mid-word at one line, because the inherited sheet-button `nowrap` defeated the line-clamp. | frame | The sheet `nowrap` targets action buttons only. The notification body keeps its 2-line clamp. |
| 49f5e835a, ef59992a7, cc586b34c | PR | KPI values and hints were clipped or truncated. | frame, features/procurement/kpi-value.tsx, procurement-minimal.css | KPI values and hints are never clipped at 390px. |
| ae378bc40, 3d1275ec8 | PR | The work board avatar stack shifted, and card meta was clipped. | theme, features/work-board/work-board-minimal.css | The avatar stack has a stable width. Card meta is not clipped. |
| 4b00278be, 5ad92da71, 2806c227f | PR | Mobile overflow, unwrapped empty-state copy, and clipped select and footer labels. | theme, minimal, kit/select-field.tsx, kit/table-footer.tsx, features/counts/*, features/feed/feed-analytics.tsx | No horizontal overflow. Empty-state copy wraps. Select and footer labels are not clipped. |
| 99590f73b | PR | Filter fields overlapped each other. | frame | Sibling filter fields never overlap (sibling-overlap check). |
| be6e7b968, a564dfece | main | The Ask Mesha panel covered the top bar controls, and its launcher sat over bottom toolbars. | features/ceo-ai/ceo-ai-panel.tsx (template chat, FIXJ9) | The panel sits below the app top bar and above page content. The launcher clears bottom toolbars. |
| c1f2d5190 | main | The Ask Mesha panel did not scroll. | ceo-ai-panel.tsx, components/app/sections/chat | The panel body scrolls independently. |
| 7a95905f2 | main | Drawer and process chip rendering broke. | features/process-integrity/*.tsx, theme | Protocol-adherence drawer and chips render intact. |

## 6. Mobile webview

| SHA | Branch | Bug | Files | Invariant |
|---|---|---|---|---|
| 89a0c2f62, 9d1b8e324 | PR | Touch traps in the Android webview, and on the vaccination schedule. | theme, minimal, work-board-minimal.css | No element swallows touch or scroll in the webview. The schedule scrolls. |
| 56b3da919 | main | The vaccination schedule, feed and load-detail pages did not scroll on mobile. | theme, full-vaccine-schedule.tsx, feed-direction.tsx, feed-packing.tsx, load-detail.tsx | These surfaces scroll vertically in the webview. |
| c8accb2b3 | PR | Drawer and calendar layouts broke in the webview. | theme, features/weighing/weights-assumptions.tsx | Drawers and the calendar fit a 390px webview. |
| 3d28a6f06 | main | The phone-floor rule `table{min-width:540px}` pushed the stage price grid past a 390px drawer. | theme, weights-assumptions.tsx | Drawer grids are exempt from the table min-width floor. Refusals scroll into view. |
| 81d23e2a9, 4fa218596, 352adeb77 | PR | Work board filters escaped the webview, and mobile parity drifted. | work-board-minimal.css | Filters stay within the webview width. |
| 7cb68a618 | PR | Feed empty cards rendered blank in the webview. | feed | Empty cards render their EmptyState in the webview. |
| 7df0f03dd, 236624745, dbd4a220f, 6227d58af | main/PR | The tasks board became a 4500px vertical stack at 390px. The pager and filter chips broke. | theme, frame | The phone board is a horizontal scroll-snap row with each column min(85vw, 100% - 28px). The pager stays on one row. Row links are 44px. |
| eb0e8ce2f, d0b48bba7, 7e663df70 | main/PR | Tap targets were under 40px. | theme | Every tap target is at least 40px at phone width (44px for date and park pickers). |
| 424095c53, b56c1b379 | main | Ask Mesha phone layout: the chats list was open, and minimize was missing. | ceo-ai-panel.tsx, components/app/sections/chat | Chats are closed by default behind a scrim. Minimize is visible and docks the panel as a bottom bar. |
| 02300791a, 0b86f1e68, 45648feda, 8fa2c2d70 | main/PR | Calendar config, sales loads filters, live tracker filters and people filters were unreadable on phones. | theme, features/procurement/sales-loads.tsx | Filters are compact and readable at 390px. At 640px or narrower they fold into a sheet. |
| 9e130e776 | main | Webview checks were unpinned. | tools/dashboard-automation | The mobile webview smoke checks stay pinned. |
