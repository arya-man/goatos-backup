@AGENTS.md

## Web Review Lens

Follow `AGENTS.md` and `docs/agent-rules/ui-frontend.md` for all web/admin-web reviews: any change touching web UI,
CSS, routes, page contracts, or visible copy must be reviewed on both laptop and
mobile. Include nested tabs, drawers/modals/popovers, dynamic detail pages,
sidebars, charts, tables, and scroll regions. A green build/typecheck alone is
not enough; run or require the responsive visual guard and inspect screenshots
before claiming UI proof.

Component-level visual regression is the third gate, and it applies to EXISTING
and FUTURE UI work alike. Any change to `components/kit/**`, `features/**` or a
page's visual shell must ship a Storybook story in `apps/admin-web/stories/`
covering its real states plus a 390px variant for every table, popup/modal/drawer,
tab strip, pagination control and labelled chart, and must pass
`npm --prefix apps/admin-web run smoke:stories:baseline` (every story at 1440x900
AND 390x844, dark AND light, play/interaction functions included) before pushing.
Baselines live in `.codex-goatos-render/admin-web-story-baselines/` and are
rewritten only for an intended change with
`npm --prefix apps/admin-web run smoke:stories:update-baseline`, with the changed
PNGs opened and the reason stated. `npm --prefix apps/admin-web run smoke:visual:all`
runs the component lane plus the route baselines. Details:
`apps/admin-web/AGENTS.md` -> "Component visual regression (Storybook)".

Mobile/WebView proof is a separate, machine-enforced gate on top of that. Before
any admin-web UI push, run `npm --prefix apps/admin-web run smoke:webview:static`
and, against a live app, `npm --prefix apps/admin-web run smoke:webview` — every
smoke route at 1440x900 and a Pixel 5 profile (393x851, Android UA, touch) in both
themes. Chart labels, tables, modals, drawers, pagination and tap targets are
verified at phone width, with the 393px screenshots opened, before you claim the
change is done. Read `.agents/skills/mobile-webview-guard/SKILL.md` first; do not
grow `scripts/check-mobile-webview-waivers/mobile-webview-waivers.json` to make a
change land.

The design-system contract is a third machine gate. Before any admin-web UI push
read `.agents/skills/design-system/SKILL.md` and run
`npm --prefix apps/admin-web run design:guard` (static: brand lock, banned
patterns, `loading.tsx` beside every admin page, PageShell adoption),
`npm --prefix apps/admin-web run visual:stories` (EVERY Storybook story at 1440 +
390, dark + light, interaction frames, render-integrity probe) and, against a
live app, `npm --prefix apps/admin-web run visual:routes` (desktop / phone /
Android-WebView profiles, both themes, same probe). Baselines move only via the
`:update-baseline` scripts with the changed PNGs opened; a grown waiver file
(`scripts/check-design-system-waivers/`, `visual-baselines/*/waivers.json`) is a
review finding. Colour literals, Tailwind palette classes, native `<select>`,
`window.confirm`, prose under titles and "F2" are refused outright.

MUI Minimal is the visual reference for every admin-web page. `~/mesha/mui/Minimal_TypeScript_v7.7.0`
lives on Ravi's laptop (licensed source, **NOT** committed). Every admin-web area maps to a
template SECTION in `docs/design/route-template-map.json`; a NEW page must add its area in the same
change or fail `route-template-map-missing`. Charts must be Apex (`components/minimal/chart` or
`components/kit`) or the two inline helpers (`svg-bars`, `svg-series`) — `raw-chart-lib` refuses
recharts / d3 / chart.js / nivo / victory / visx / echarts / highcharts. Production bug CLASSES
(text-icon overlap, wide-table no wrapper, chart axis <11px, pinned-bar blur flicker,
drawer-filter mismatch, chart hover re-mount) are automated checks in
`apps/admin-web/scripts/lib/visual-pattern-guards.mjs`. Full pattern → guard table + how-to-add-a-page
ordering: `docs/design/README.md` §5b + §5c.

## Hard Local Resource Rule

For Goat OS on Ravi's laptop, do not start Colima, Docker Desktop,
`goatos-local-current`, or local Docker Postgres when the OCI Postgres tunnel on
`127.0.0.1:15432` is available. Do not run commands like `colima start`,
`docker start goatos-local-current`, or Postgres integration tests with
`GOATOS_RUN_POSTGRES_TESTS=1` unless Ravi explicitly asks for a local Docker DB,
Docker-specific test, or disposable mutation database.

## Vaccination Anchor Dates

Follow the vaccination anchor-date rule in `docs/agent-rules/vaccination.md` and the detailed runbook
in `docs/preventive-care-vaccination/vaccination-anchor-runbook.md`. In short:
an anchor date is baseline vaccine history/start-date semantics for the selected
animals, not a blind one-off drive insert. Use the vaccination kernel/generation
path, verify live/live and live/killed spacing plus same-day caps, and report
actual RFID/tag identifiers rather than internal goat ids. `Z1+Z3` is one
vaccine/program label, not separate `Z1`, `Z2`, and `Z3` stages.

For vaccination drive packing, follow the 200-animals-per-operator-day rule in
`docs/agent-rules/vaccination.md` and `docs/preventive-care-vaccination/vaccination-rules.md`: pack
complete sheds first, keep sibling partitions under the same parent shed
together when they fit, and do not split a whole shed/group merely to fill the
last seats under 200.
