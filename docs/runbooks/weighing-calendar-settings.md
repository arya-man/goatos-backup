# Weighing calendar settings

The Weights and ADG Analytics period defaults and earliest selectable day come from the tenant's **published Weighing SOP** in Postgres. The Download calendar uses the same earliest day. No environment variable or frontend rebuild is needed to change these values after this feature is deployed.

## Change the settings

1. Open **Weighing → Weighing SOP** (`/weighing/sops`).
2. Open the published SOP, choose **Change SOP**, and find **Weights pages**.
3. Set the earliest calendar day independently of the default start:
   - Initial earliest day: **2026-07-05** (July 5).
   - Initial default mode: **Fixed date**, **2026-08-03** (August 3).
   - For a moving start, choose **Rolling weeks** and enter a whole number from **1 to 520**.
4. Publish the SOP. Saving a draft alone does not change reporting pages.
5. Reload ADG Analytics or Weights without `wt_from` / `wt_to` query parameters. Existing explicit date selections are preserved; clear them or open the navigation link to use the new default.

The bootstrap contract is cached for up to 60 seconds in the frontend process. If a page still shows the previous configuration immediately after publishing, wait for that cache to expire and reload. A code deploy or server restart is unnecessary. Backend SOP family revisions invalidate the compiled contract when a version is published.

## Date semantics

All arithmetic uses the current **Asia/Kolkata business date**. In `rolling_weeks`, the default start is `today - (weeks × 7 days)`; it is not based on the latest weighing date. On September 16, 2026:

| Setting | Default start |
| --- | --- |
| Fixed August 3 | August 3, 2026 |
| 6 weeks before today | August 5, 2026 |
| 2 weeks before today | September 2, 2026 |

The start is clamped to the earliest selectable day. Both period endpoints are inclusive, so “6 weeks before today” describes a date offset, not an inclusive 42-date count. The default end remains the latest weighing day in the selected scope. If that day is absent, in the future, or before the configured start, the end is today so the request is never inverted. Future default starts are clamped to today.

Existing `rolling_days` configurations remain supported with their original inclusive-day behavior (`today - (days - 1)`), and can still be edited. They are not silently converted to weeks.

## Storage and contracts

The authority is the highest published `weighing.session` version for the tenant, stored in `sop_versions.form_dsl.weighing.weights_pages`. Example initial value:

```json
{
  "default_from_mode": "fixed_date",
  "default_from_date": "2026-08-03",
  "earliest_date": "2026-07-05"
}
```

Example rolling configuration:

```json
{
  "default_from_mode": "rolling_weeks",
  "default_from_weeks": 6,
  "earliest_date": "2026-07-05"
}
```

Use the SOP editor and its version/validate/publish workflow rather than editing published JSON directly. This preserves version history, authorization, optimistic concurrency, audit, and contract revision invalidation. Backend validation rejects fractional, zero, negative, or more than 520 weeks. The page settings are read from the current published SOP; changing them does not change the execution rules pinned to existing weighing tasks.

`/admin-web/bootstrap` supplies `weights.window.default_from_mode`, `weights.window.default_from_date`, `weights.window.default_from_days`, `weights.window.default_from_weeks`, and `weights.window.earliest_date` to both reporting page contracts. No additional request is added to a page. The frontend resolves the default and uses the same minimum for the period picker and export picker.

The forward migration widens only the old seeded calendar configuration and materializes missing page defaults when the weighing rules block already exists, while preserving custom settings, drafts, and retired versions. An older version without the entire weighing rules block uses the seeded fallback until its next normal SOP publication. The migration is forward-only: rollback must not overwrite subsequent operator changes.

## Focused validation

```sh
node --experimental-strip-types --test apps/admin-web/features/weighing/landing-window-behavior.test.mjs apps/admin-web/features/weighing/weights-window.test.mjs apps/admin-web/features/sops/weighing-model.test.mjs
(cd backend && go test ./internal/weighing/domain ./internal/weighingsop/... ./internal/adminui/app ./internal/adminui/adapters/postgres)
npm --prefix apps/admin-web run check:ui-contract
```

Live proof must use the actual OCI-backed page with data: July 4 disabled / July 5 enabled, a July 5 selection reaching the URL and backend, unchanged fixed August 3 landing, configurable six/custom-week publication taking effect without restart, and restoration of fixed August 3. Check the SOP editor as well as the analytics, weights, and export calendars. Fail on `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, and `Weights could not be loaded`. Inspect screenshots before presenting them as evidence.
