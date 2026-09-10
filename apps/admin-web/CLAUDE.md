@AGENTS.md

## Responsive Review Lens

For any admin-web review or code change touching rendered UI, routes, CSS,
page contracts, charts, tables, or visible copy, apply both laptop and mobile
UI/UX review. Run or require `npm run responsive:guard`, keep
`scripts/smoke-visual-live.mjs` and
`scripts/smoke-visual-route-coverage.test.mjs` updated for every page, nested
tab, drawer/modal/popover state, and dynamic detail route, and visually inspect
the generated screenshots before claiming proof.
