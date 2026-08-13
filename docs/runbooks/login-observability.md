# Login Observability

Use this runbook when a tester says they cannot log in or keeps sending the
Android "Could not load workspace" screen. Do not infer login state from a
screenshot alone.

## Evidence Model

Android emits/logs these states:

- `login_session_ready`: Firebase signed in and issued an ID token. Includes
  `email` and `firebase_uid`.
- `login_success`: Goat OS accepted the session event and the app opened its
  local session. Includes `email` and `firebase_uid`.
- `bootstrap_loaded`: `/app/bootstrap` returned and the shell has backend-owned
  navigation. Includes `email`, `firebase_uid`, and `chrome`.
- `bootstrap_failed`: the app had a local session but workspace bootstrap
  failed. Includes `email`, `firebase_uid`, and a coarse `reason`.

Backend Cloud Logging emits these states:

- `auth_succeeded`: bearer token verified for a request. Includes `email`,
  `firebase_uid`, `actor_id`, `tenant_id`, method, and path.
- `auth_failed`: bearer token or auth context failed. Includes path, status,
  and error `code` such as `missing_bearer_token`.
- `auth_session_event_succeeded`: `/auth/session-events` recorded sign-in,
  refresh, or sign-out. Includes `event_type`, `email`, `firebase_uid`,
  `actor_id`, and pending-grant claim counts.
- `auth_session_event_failed`: session-event audit write failed after token
  verification.
- `app_bootstrap_succeeded`: `/app/bootstrap` resolved workspace, role, nav,
  modules, and device state.
- `app_bootstrap_failed`: `/app/bootstrap` failed after auth, with application
  error `code` such as `operator_profile_missing`, `operator_grant_missing`, or
  `device_revoked`.
- `app_device_register_succeeded` / `app_device_register_failed`: Android
  device registration outcome.

Admin web emits browser console events (`admin_firebase_login_*`,
`admin_firebase_session_sync_*`) and server logs (`admin_auth_session_*`,
`admin_auth_backend_event_*`) around the same sign-in/session bridge.

## Query A User

Replace the email and optional Firebase UID:

```bash
EMAIL="manju@mesha.sg"
FIREBASE_UID="Iz3I6SC3ZTeAjFJQ7sqjb6ZLSHB3"

gcloud logging read \
  'resource.type="cloud_run_revision"
   AND resource.labels.service_name="goatos-api-stg"
   AND timestamp>="2026-07-31T00:00:00Z"
   AND (
     jsonPayload.email="'$EMAIL'"
     OR jsonPayload.firebase_uid="'$FIREBASE_UID'"
     OR jsonPayload.msg="auth_failed"
     OR jsonPayload.msg="app_bootstrap_failed"
   )' \
  --project=goatos-stg \
  --limit=200 \
  --format=json
```

Useful failure codes:

- `missing_bearer_token`: app/browser called backend without an ID token.
- `invalid_bearer_token`: token was present but rejected.
- `email_not_allowed`: Firebase account is not allowed for this environment.
- `missing_tenant_context`: request lacked a Goat OS tenant id.
- `permission_denied`: token verified, but DB grants did not authorize route.
- `operator_profile_missing`: mobile profile row missing.
- `operator_grant_missing`: mobile profile exists but no active grant.
- `device_revoked`: app sent a revoked device id.

## Interpretation

For Android, the user is truly through the login gate only when all are present
in order:

1. `login_session_ready` or backend `auth_session_event_succeeded` for
   `auth.sign_in`;
2. backend `app_bootstrap_succeeded`;
3. Android `bootstrap_loaded`.

If `auth_succeeded` exists but `app_bootstrap_failed` follows, fix DB/profile/
grant/device state. If `auth_failed missing_bearer_token` appears, fix the
client token/session race or ask the tester to sign in again after updating to
the fixed build.

## Firebase Analytics Export

App-side Firebase Analytics calls are useful but are not the operational source
of truth until the STG Firebase GA4 BigQuery export dataset is linked and the
analytics rollup job is configured with `GOATOS_GA4_EXPORT_DATASET`. Backend
Cloud Logging is the immediate source of truth for login triage.
