# Production-facing Android release

This is the current public/operator-facing release contract while GoatOS reuses
the existing `goatos-stg` Firebase/GCP project internally.

## Public identity

```text
App package: sg.mesha.goatos
Dashboard:   https://dashboard.mesha.sg
API:         https://api.goatos.mesha.sg/
Version:     no -stg suffix
```

Do not use `sg.mesha.goatos.stg`, `stg.dashboard.mesha.sg`,
`stg-api.dashboard.mesha.sg`, or `-stg` version names in public release notes,
APK filenames, dashboard links, operator instructions, or app-distributed
metadata for this path.

## Firebase

For the current path, reuse the existing `goatos-stg` Firebase project:

1. Open Firebase Console for project `goatos-stg`.
2. Add an Android app with package name `sg.mesha.goatos`.
3. Download that app's `google-services.json`.
4. Place it at `apps/goatos-android/app/src/prod/google-services.json`.
5. Set `FIREBASE_APP_ID` to the downloaded file's
   `client_info.mobilesdk_app_id` before running mobile distribution.
6. If the build needs generated-equivalent XML, add matching values under
   `apps/goatos-android/app/src/prod/res/values/firebase.xml`.

The backend must keep trusting the existing Firebase issuer/audience while this
project is reused:

```text
GOATOS_AUTH_ISSUER=https://securetoken.google.com/goatos-stg
GOATOS_AUTH_AUDIENCE=goatos-stg
```

That `goatos-stg` value is internal auth plumbing. It must not leak into public
URLs, release labels, package names, or operator-facing instructions.

## DNS

Cloudflare should expose production-facing names only:

```text
dashboard.mesha.sg
api.goatos.mesha.sg
```

Create DNS-only A records in the `mesha.sg` Cloudflare zone after the Google
load balancer target is confirmed:

```text
dashboard.mesha.sg   -> 8.233.143.24
api.goatos.mesha.sg  -> 8.233.143.24
```

Do not delete legacy staging DNS until the production-facing app, web login, API
bootstrap, and mobile login are verified.

## Google Load Balancer And Auth

Before deploying backend/web with production-facing names, the existing
`goatos-stg` load balancer must accept the new hosts:

```text
Managed certificate: ACTIVE for dashboard.mesha.sg
Managed certificate: ACTIVE for api.goatos.mesha.sg
URL map: goatos-stg-dashboard-map routes api.goatos.mesha.sg -> goatos-api-stg-backend
Default URL-map backend continues to serve goatos-admin-web-stg for dashboard.mesha.sg
```

Firebase/Auth Platform authorized domains must include `dashboard.mesha.sg`.
The Google OAuth web client must include this JavaScript origin and redirect:

```text
https://dashboard.mesha.sg
https://dashboard.mesha.sg/api/auth/google-redirect
```

The deploy preflight intentionally fails before image rollout if DNS, managed
certificates, or the API host rule are missing.

## Preflight

Run these checks before mobile distribution:

```bash
dig +short dashboard.mesha.sg A
dig +short api.goatos.mesha.sg A
gcloud compute ssl-certificates list \
  --project=goatos-stg \
  --global \
  --format='table(name,managed.domains,managed.status)'
gcloud compute url-maps describe goatos-stg-dashboard-map \
  --project=goatos-stg \
  --global \
  --format=json
jq -er '.client[] | select(.client_info.android_client_info.package_name == "sg.mesha.goatos") | .client_info.mobilesdk_app_id' \
  apps/goatos-android/app/src/prod/google-services.json
```
