# Incident: Jyothi (verifier) — STG mobile login 403

- **Date:** 2026-07-25
- **Environment:** `goatos-stg`
- **Reporter:** maintainer (mobile app)
- **Impact:** one user (Jyothi, `verifier`) could not sign in to the Android
  app. No other users affected. No data loss.
- **Status:** RESOLVED (config-only fix; no backend deploy, no Firebase change).

## Symptom

Jyothi signed in on the Android app with her Firebase email/password
(`jyothipvg12345@gmail.com`) and got an HTTP **403**. The maintainer had already
given her the `verifier` role, so the role assignment was not the problem.

Observed request pattern in the stg API logs:

```
POST /auth/session-events   -> 403
GET  /app/bootstrap         -> 401
```

## Root cause (the real blocker)

**Her email was not in the stg auth email allowlist.**

`/auth/session-events` verifies the Firebase token FIRST, then checks the email
against the allowlist (`authaudit.Handler.RecordSessionEvent`,
`backend/internal/platform/authaudit/handler.go`). Because the failure was
`email_not_allowed` and **not** `invalid_bearer_token`, we know her Firebase
token, issuer (`https://securetoken.google.com/goatos-stg`), and audience
(`goatos-stg`) were all valid — the Firebase side was never the problem.

The allowlist is the env var `GOATOS_AUTH_ALLOWED_EMAILS` on `goatos-api-stg`,
sourced from Secret Manager secret **`goatos-stg-auth-allowed-emails`**. That
secret held all 10 other people (5 leadership + Amit/Darshan/Sagar operators +
Chandrakant director + one e2e account) but **omitted Jyothi's email**. She was
the 10th canonical STG login person (see the personnel rule in
`docs/runbooks/auth.md`), but the allowlist secret was never updated from 9 → 10
for her.

Confirmed from `audit_log`: every attempt recorded
`action=auth.failed_sign_in`, `reason=email_not_allowed`,
`email=jyothipvg12345@gmail.com`.

## Secondary gap (found while diagnosing)

Her `workforce_members` row had an **empty `department_id`**. Module access
(the vaccination/verify surface on the mobile bottom bar) is granted per
**department** via `department_module_grants`, keyed off the department — not off
the role. The 3 operators + the director are all bound to department
`preventive_care`; Jyothi was not. Even after the allowlist fix she would have
signed in but seen no vaccination/verify module.

Cause: `seed-stg-login-grants` binds a department only for accounts that carry a
`DepartmentCode` **and** a `RosterDisplayNameMatch` (the operators/director,
which have named `seed-roster-real` rows). The verifier account had neither, so
it fell to the leadership-style `ensureAuthProfileMember` path, which creates an
`auth:<uid>` profile with **no department**.

## Fixes applied (both config-only, no deploy)

1. **Allowlist (the actual unblock).** Added `jyothipvg12345@gmail.com` to secret
   `goatos-stg-auth-allowed-emails` (new version 6), then rolled a new
   `goatos-api-stg` revision so the env-var secret is re-read (env-var secret
   refs are resolved at revision start, so a new version needs a fresh revision):

   ```bash
   # append her email, add a new secret version
   gcloud secrets versions add goatos-stg-auth-allowed-emails \
     --project=goatos-stg --data-file=-   # <current>,jyothipvg12345@gmail.com
   # force a new revision that reads :latest
   gcloud run services update goatos-api-stg \
     --project=goatos-stg --region=asia-south1 \
     --update-secrets=GOATOS_AUTH_ALLOWED_EMAILS=goatos-stg-auth-allowed-emails:latest
   ```

2. **Department (so she sees the verify module).** Bound her existing
   `workforce_members` row to the `preventive_care` department
   (`beae3da5-9c77-40e5-897c-2c7a302ef45c`) directly in the stg DB. Binding a
   department gives module visibility; it does **not** add vaccination operator
   capacity (capacity is role-driven — `verifier` contributes none).

## Verification

After the fix, from the live stg auth log / `audit_log`:

```
POST /auth/session-events   -> auth.sign_in (SUCCESS, no failure reason)
GET  /app/bootstrap         -> 200
```

She is signed in with the vaccination/verify module available. Confirmed against
the real login, not a synthetic token.

## "Deploy or Firebase?" — answer for future incidents of this shape

Neither. A `403 email_not_allowed` is a **stg config** problem
(allowlist secret + one revision roll), not a backend code deploy and not a
Firebase user/token problem. The `403` only fires after successful token
verification, so if you see it, the Firebase side is already working — do not
re-issue tokens, re-create the Firebase user, or redeploy the API image.

## Prevention (follow-ups)

- **P1 — allowlist membership check.** The STG login verification runbook must
  assert that ALL 10 canonical emails (Jyothi included) are present in
  `goatos-stg-auth-allowed-emails`. A person is not "login-ready" on STG until
  their email is in that secret AND their grant is materialized AND (for module
  users) their department is bound. Docs updated in this change:
  `docs/runbooks/auth.md`, `docs/runbooks/stg-login-seed-contract.md`,
  `docs/runbooks/stg-9-person-login-verification.md`.
- **P1 — seeder department gap (code follow-up, not in this change).**
  `backend/cmd/seed-stg-login-grants` should bind the `verifier` account to the
  `preventive_care` department on reseed so this does not silently regress. The
  operator/director bind path assumes a named `seed-roster-real` roster row and
  deactivates the `auth:<uid>` placeholder first; the verifier has no named
  roster row, so it needs its own path: ensure the `auth:<uid>` profile WITH a
  department and grant the department modules, WITHOUT operator capacity. Track
  as a scoped backend change with a unit test for the verifier path.
- **B — cross-module verifier model gap (design follow-up).** A `verifier` is
  conceptually a cross-module role (she verifies whatever produces proof
  videos). Today module access hangs off a single `workforce_members.
  department_id`, so a verifier can only see ONE department's modules. Right now
  only vaccination has an operator-video → verify flow, so binding her to
  `preventive_care` covers 100% of what exists. When counts/feed/breeding add
  video proof, a single department binding will NOT auto-extend her reach. The
  proper fix is to derive a verifier's module access from the role (or a
  multi-department binding) rather than one `department_id`. This is a backend
  model change, not a data fix. Do not paper over it by inventing per-module
  verifier rows.
