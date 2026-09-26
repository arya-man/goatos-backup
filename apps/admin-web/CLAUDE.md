@AGENTS.md

## Responsive Review Lens

For any admin-web review or code change touching rendered UI, routes, CSS,
page contracts, charts, tables, or visible copy, apply both laptop and mobile
UI/UX review. Run or require `npm run responsive:guard`, keep
`scripts/smoke-visual-live.mjs` and
`scripts/smoke-visual-route-coverage.test.mjs` updated for every page, nested
tab, drawer/modal/popover state, and dynamic detail route, and visually inspect
the generated screenshots before claiming proof.

## UI Must Match The MUI Minimal Kit

Read `AGENTS.md` -> "UI must match the MUI Minimal kit" before any UI change. Kit components +
`app/minimal-tokens.css` tokens only; spec `docs/design/mui-minimal-spec.md`; add a component as
spec -> kit -> story -> baseline; a restyle never changes behaviour; never reopen a regression-guard
item; phone tap targets >= 44px and fixed overlays via `BodyPortal`. `npm run design:guard` enforces it.
