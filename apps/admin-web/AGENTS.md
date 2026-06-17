# Admin Web Agent Context

Read first:

- `../../context/frontend/final-frontend-mobile-backend-architecture.md`
- `../../context/analytics/final-analytics-infra.md`

Purpose:

- Snapshot of the current CEO/admin dashboard UI for safe Goat OS rewiring.
- The live `../../dashboard/` repo is not touched.

Do:

- Preserve useful layout, charts, route inventory, and UX patterns.
- Move data access behind generated analytics/app clients.
- Gate pages by server-side auth/RBAC.
- Treat frontend visual QA as a release gate, not a courtesy check.
- If the user is checking the Google dev URL, a Git push is not enough. Do not
  say the live dev UI is fixed until `goatos-admin-web-dev` has been rebuilt,
  pushed as a `linux/amd64` image, deployed to Cloud Run, and verified by
  reading the live service revision/image. Follow `docs/runbooks/containers.md`
  "Admin-web dev deploy checklist".
- After any frontend code change, run the relevant lint/typecheck/build plus
  the live visual smoke when local backend/admin-web can be started:

  ```bash
  npm run smoke:visual:live
  ```

- Open the generated screenshots under
  `.codex-goatos-render/admin-web-screenshots/` and inspect every touched route.
  Do not report "verified in Chrome" unless screenshots were actually reviewed.
- For pages with a legacy counterpart, open the legacy dashboard in another tab
  and compare the local page against it before pushing. For counts, use
  `https://dashboard--goatos-sheets.us-central1.hosted.app/counts/overall`.
- Check sidebar/nav label alignment, tab/title spacing, typography, colors,
  margins, padding, card geometry, chart sizing, graph labels, icons, empty
  space, overflow, clipping, desktop/narrow responsive states, and whether any
  error/config page is being mistaken for a real UI proof.
- Keep peer dashboard panels visually even by default. Two cards that represent
  equal-priority queues, summaries, or status panels must use equal grid columns
  and aligned card edges/heights at desktop breakpoints. Do not use arbitrary
  weighted fractions such as `1.05fr/0.95fr` for peer cards unless the layout is
  intentionally master/detail and the screenshot review calls that out.
- `npm run smoke:visual:live` includes layout geometry checks, serious/critical
  axe checks, screenshot capture, token-leak checks, and optional visual
  baseline diffing. Build/typecheck passing is not enough for frontend work.
- Use the baseline commands when a visual baseline exists or when establishing a
  local comparison set:

  ```bash
  npm run smoke:visual:update-baseline
  npm run smoke:visual:baseline
  ```

Do not:

- Do not add direct BigQuery/Sheets/GCS/DB access as the final data path.
- Do not expose unauthenticated real goat data.
- Do not treat this copy as proof that live dashboards have changed.
- Do not confuse `git mesha-push main` with a Google dev deploy. Pushed code is
  only in Git; the raw Cloud Run URL still serves the previous image until the
  admin-web image is rebuilt and the service is redeployed.
- Do not use a `missing_config`, token error, blank page, or console-only check
  as visual QA evidence.
