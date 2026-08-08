# Goat OS Android changelog

## 0.1.15-stg — 2026-08-08

- Uses the latest staging backend/admin runtime from `origin/main` at `188975a7cd73`.
- Includes the admin root-route fix that routes verifier home away from the control tower.
- Keeps the signed staging package on `sg.mesha.goatos.stg`.
- Points the app at `https://stg-api.dashboard.mesha.sg/`.

## 0.1.14-stg — 2026-08-08

- Uses the latest staging backend/admin runtime from `origin/main` at `fbd61bd7448`.
- Includes the feed fix for frozen sheets with partitioned sheds.
- Keeps the signed staging package on `sg.mesha.goatos.stg`.
- Points the app at `https://stg-api.dashboard.mesha.sg/`.

## 0.1.13-stg — 2026-07-29

- Uses the latest staging backend/admin runtime from `origin/main`.
- Includes the Weighing sync/outbox validator fix and sync sheet behavior update.
- Keeps the signed staging package on `sg.mesha.goatos.stg`.
- Points the app at `https://stg-api.dashboard.mesha.sg/`.

## 0.1.2-stg — 2026-07-21

- Uses the latest staging backend/admin runtime deployed from `origin/main`.
- Keeps the signed staging package on `sg.mesha.goatos.stg`.
- Points the app at `https://stg-api.dashboard.mesha.sg/`.
- Includes the CEO/CXO SSO bootstrap provisioning fix and staging release-signing guardrails.

## 0.1.1-stg — 2026-07-21

- Points the staging app at `https://stg-api.dashboard.mesha.sg/`.
- Uses the `sg.mesha.goatos.stg` package and `goatos-stg` Firebase project.
- Includes the latest staging authentication/session bootstrap fixes from `origin/main`.
- Matches the cleaned staging seed: 1,311 goat rows, 1,308 active goats, verified vaccination/HRMS seed state.
