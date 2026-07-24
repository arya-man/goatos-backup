# Auth Runbook

Goat OS has two backend token-verification modes:

```text
GOATOS_AUTH_MODE=bearer  # HS256, local/dev/test only
GOATOS_AUTH_MODE=jwks    # asymmetric RS256/ES256, required for shared envs
```

## Local/dev HS256

HS256 bearer mode exists for throwaway local rehearsal and deterministic smoke
tests. It requires:

```text
GOATOS_ENV=local|dev|test
GOATOS_AUTH_MODE=bearer
GOATOS_AUTH_HS256_SECRET=<strong local secret>
GOATOS_AUTH_ISSUER=<issuer>
GOATOS_AUTH_AUDIENCE=<audience>
```

The API rejects HS256 when `GOATOS_ENV` is missing or is a shared environment
such as `stg`, `prod`, or `production`.

## Shared/staging/production JWKS

Shared environments must use asymmetric JWKS verification:

```text
GOATOS_ENV=dev|stg|prod
GOATOS_AUTH_MODE=jwks
GOATOS_AUTH_ISSUER=<idp issuer URL>
GOATOS_AUTH_AUDIENCE=<goat-os api audience>
GOATOS_AUTH_JWKS_URL=<idp JWKS endpoint>
GOATOS_AUTH_ALLOWED_ALGS=RS256,ES256
GOATOS_AUTH_ALLOWED_EMAILS=<approved admin email>[,<approved admin email>...]
```

Firebase App Check is a second, app-attestation layer for shared Android
traffic. It is off by default and applies only to protected bearer-auth routes:

```text
GOATOS_APPCHECK_ENFORCE=off|monitor|enforce
GOATOS_APPCHECK_ISSUER=https://firebaseappcheck.googleapis.com/<firebase project number>
GOATOS_APPCHECK_AUDIENCE=projects/<firebase project number>
GOATOS_APPCHECK_JWKS_URL=https://firebaseappcheck.googleapis.com/v1/jwks  # optional default
GOATOS_APPCHECK_CLOCK_SKEW=60s                                             # optional
GOATOS_APPCHECK_JWKS_CACHE_TTL=5m                                          # optional
```

Use `monitor` after the Android app sends `X-Firebase-AppCheck`; it logs
missing/invalid/pass without blocking old clients. Use `enforce` only after the
Firebase App Check console registration and Android rollout are healthy.

Authorization still comes from active `user_scope_grants` rows in Goat OS; token
claims authenticate the subject but do not grant product roles by themselves.
`GOATOS_AUTH_ALLOWED_EMAILS` is required in JWKS mode; the API fails closed at
startup if it is empty. The backend rejects verified Firebase/JWKS tokens unless
the token email is verified and present in that email allowlist. This blocks
session-cookie creation through `/auth/session-events` and blocks every
protected API request before RBAC grant lookup.

Current access-control layers:

1. Firebase/Auth Platform verifies the identity and returns a Firebase ID token.
   Shared dashboard login supports both Google SSO and Firebase
   email/password. Goat OS never stores raw Google, password, or Firebase tokens
   in the database.
2. Firebase App Check, when enabled, verifies `X-Firebase-AppCheck` against
   Firebase App Check JWKS before RBAC grant lookup. App Check attests the app;
   it does not identify or authorize the user.
3. `GOATOS_AUTH_ALLOWED_EMAILS` is the deploy-time email allowlist. It blocks
   unapproved verified emails before a session cookie is created.
4. `auth_pending_email_grants` is the approved-email grant policy table. On a
   verified `auth.sign_in`, the backend converts a matching email policy into a
   real active `user_scope_grants` row for the Firebase/JWKS subject. Raw
   tokens are never stored.
5. `user_scope_grants` is the database authorization table that protected APIs
   actually check.
6. `audit_log` records sign-in, refresh, sign-out, failed sign-in, and
   `auth.pending_email_grant_claimed` events
   with metadata such as verified email and Firebase UID; raw tokens are never
   written.

The dev allowlist and pending email grants are intentionally operator-managed
until the product has a real admin user-management screen. Do not treat Google
Workspace membership or the Google provider `hd` hint as sufficient access
control.

## Firebase Dashboard Sign-In

`goatos-dev` and `goatos-stg` use Google Identity Platform / Firebase Auth as
the real IdP for JWKS verification. Firebase is auth only for Goat OS bring-up:
do not use Firebase Hosting or Firebase App Hosting, and do not introduce an
external OIDC provider or static dev JWKS.

Admin-web login uses Google Identity Services for the browser account chooser,
then exchanges the returned Google ID token with Firebase Auth using
`signInWithCredential`. Do not use Firebase `signInWithPopup` or
`signInWithRedirect` for this dashboard: both send the browser through
`goatos-dev.firebaseapp.com/__/auth/handler`, which has timed out in live dev
testing. Keep `auto_select=false` and `hd=mesha.sg`; do not enable One Tap for
this internal dashboard without proving account-picker behavior on
`https://dev.dashboard.mesha.sg`.

Admin-web also supports Firebase email/password sign-in and password-reset
email from the same login page. This is still Firebase Auth, not a Goat OS
password table. Operators must enable the Firebase email/password provider in
the target Google project before deploy, create users in Firebase/Auth Platform,
and keep `GOATOS_AUTH_ALLOWED_EMAILS` plus `auth_pending_email_grants` aligned
with approved dashboard users.

For initial seed users, do not create or share a common password. Use the admin
seed script to create or verify the approved Firebase/Auth Platform users and
send each person a password-reset email:

```bash
npm --prefix apps/admin-web run auth:seed-password-users -- \
  --project goatos-stg \
  --user ravi@mesha.sg=Ravi \
  --user manohark@mesha.sg=Manohar \
  --user manju@mesha.sg=Manju \
  --user abhishek@mesha.sg=Abhishek \
  --user aryaman@mesha.sg=Aryaman \
  --send-reset-email
```

The script uses the active `gcloud` OAuth credential for Identity Platform
admin APIs, refuses to run if the active project differs from `--project`, never
prints the generated temporary password, marks approved seed emails verified so
Goat OS JWKS email verification passes, and sends Firebase password-reset
emails so every user chooses their own password. Use `--dry-run` first when
reviewing a new environment. Add `--continue-url http://localhost:3300/login`
for local testing or `--continue-url https://stg.dashboard.mesha.sg/login` once
the staging custom domain is live.

### Password-reset email host and spam handling

Firebase/Auth Platform does not automatically use `stg.dashboard.mesha.sg` in
password-reset emails just because that host is an authorized domain. Authorized
domains allow the host to participate in auth flows; the email action link is
controlled separately by the Firebase Auth email template. If the template is
left at its default, reset emails use the project handler:

```text
https://goatos-stg.firebaseapp.com/__/auth/action
```

For `goatos-stg`, the desired dashboard-host action handler is:

```text
https://stg.dashboard.mesha.sg/__/auth/action
```

The admin-web route at `/__/auth/action` verifies the Firebase `oobCode` and
completes password reset on the dashboard host. Keep the URL path exactly as
`/__/auth/action`; Firebase appends `mode`, `oobCode`, `apiKey`, and optional
`continueUrl` query parameters. Admin-web and Android reset requests now pass a
dashboard `continueUrl`; for seed-script reset emails, include:

```bash
--continue-url https://stg.dashboard.mesha.sg/login
```

Deliverability is separate from the visible action-link host. To stop Gmail from
treating reset mail as generic `firebaseapp.com` mail, configure a Mesha-owned
sender domain in Firebase Auth Templates and add the DNS records that Firebase
provides (`TXT`/`CNAME`, including SPF/DKIM as shown in the console). The DKIM
`CNAME` records must stay **DNS-only** in Cloudflare. If Cloudflare asks whether
to turn on proxy status for those records, click **Cancel**.

The sender-domain setting improves the `From` domain and SPF/DKIM alignment, but
it does not reliably change `%LINK%` by itself. If Firebase-sent emails still
show the default action URL, the remaining options are:

1. Firebase support or the Firebase Console successfully applies the reset
   template custom action URL to the dashboard handler.
2. A Firebase Hosting-backed custom link domain is configured, with Hosting
   rewriting `/__/auth/action` to admin-web, and clients pass `linkDomain`.
3. Goat OS sends reset emails through a Mesha-owned mail provider after
   generating an OOB code/link and constructing the dashboard-host URL.

Do not set the Android `goatosAuthActionLinkDomain` build property until Firebase
recognizes that domain as a valid custom Firebase Hosting link domain. Neither
the Firebase template path nor the custom mail-sender path requires database
seeding.

#### Staging reset-link remediation record

On 2026-07-11, staging was remediated up to the Firebase-controlled action-link
boundary:

1. Reauthenticated Google tooling as `ravi@mesha.sg` with browser flows after
   token refresh failed:

   ```bash
   gcloud auth login ravi@mesha.sg
   firebase login --reauth --no-localhost
   ```

2. Verified the target before any cloud mutation:

   ```bash
   REPO_ROOT="$(git rev-parse --show-toplevel)"
   gcloud auth list --filter=status:ACTIVE --format='value(account)'
   gcloud config get-value project
   gcloud projects get-ancestors goatos-stg --format='table(ID,TYPE)'
   git -C "${REPO_ROOT}" remote -v
   ```

   Expected staging context:

   ```text
   account:  ravi@mesha.sg
   project:  goatos-stg
   folder:   188649904255
   org:      563962826703 (vgoats.com)
   repo:     https://github.com/vgoats/goatos.git
   ```

3. Added the app-side reset handler:

   ```text
   apps/admin-web/lib/auth/firebase-client.ts
   apps/admin-web/app/auth/action/page.tsx
   apps/admin-web/components/auth/password-reset-action.tsx
   apps/admin-web/next.config.mjs
   apps/admin-web/proxy.ts
   apps/goatos-android/app/build.gradle.kts
   apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/auth/AuthRepository.kt
   ```

   The important behavior is:

   ```text
   Firebase reset request continue URL: https://stg.dashboard.mesha.sg/login
   Public handler route:                 /auth/action
   Firebase-compatible rewrite:          /__/auth/action -> /auth/action
   Proxy bypass:                         /__/auth/action and /auth/action stay public
   Android linkDomain:                   leave blank until Firebase accepts a custom Hosting link domain
   ```

4. Verified the app change before deploy:

   ```bash
   npm --prefix apps/admin-web run typecheck

   (cd apps/admin-web && \
     node node_modules/eslint/bin/eslint.js \
       lib/auth/firebase-client.ts \
       components/auth/password-reset-action.tsx \
       app/auth/action/page.tsx \
       proxy.ts)
   ```

5. Built and deployed the staging admin-web image:

   ```bash
   IMAGE="asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:f91027557b85-reset-auth-20260710222203"

   docker buildx build \
     --platform linux/amd64 \
     -f apps/admin-web/Dockerfile \
     -t "${IMAGE}" \
     --push \
     .

   gcloud run deploy goatos-admin-web-stg \
     --image="${IMAGE}" \
     --region=asia-south1 \
     --project=goatos-stg \
     --quiet
   ```

   Deployed staging revision:

   ```text
   goatos-admin-web-stg-00005-vwd
   ```

6. Smoked the deployed handler:

   ```bash
   curl -sS -o /dev/null -w '%{http_code} %{url_effective}\n' \
     'https://stg.dashboard.mesha.sg/__/auth/action?mode=resetPassword&oobCode=dummy'

   curl -sS -o /dev/null -w '%{http_code} %{url_effective}\n' \
     'https://stg.dashboard.mesha.sg/auth/action?mode=resetPassword&oobCode=dummy'

   gcloud run services describe goatos-admin-web-stg \
     --region=asia-south1 \
     --project=goatos-stg \
     --format='value(status.latestReadyRevisionName,spec.template.spec.containers[0].image,status.url)'
   ```

   Both reset-handler URLs returned `200`, and Cloud Run served the image above
   at 100% traffic.

7. Checked Firebase/Auth Platform config before sender-domain work:

   ```bash
   TOKEN="$(gcloud auth print-access-token)"
   curl -fsS \
     -H "Authorization: Bearer ${TOKEN}" \
     -H "x-goog-user-project: goatos-stg" \
     "https://identitytoolkit.googleapis.com/admin/v2/projects/goatos-stg/config" \
     | jq '.notification.sendEmail.callbackUri'
   ```

   The staging action callback remained:

   ```text
   https://goatos-stg.firebaseapp.com/__/auth/action
   ```

8. Tried to update the Firebase email action URL through both the Identity
   Platform Admin API and Firebase Console:

   ```text
   attempted target: https://stg.dashboard.mesha.sg/__/auth/action
   API result:       EMAIL_TEMPLATE_UPDATE_NOT_ALLOWED
   Console result:   "An error occurred when updating action URL"
   ```

9. Configured the Firebase Auth custom sender domain for staging and added the
   Firebase-provided DNS in Cloudflare:

   ```text
   custom sender domain: stg.dashboard.mesha.sg

   TXT    stg.dashboard.mesha.sg
          v=spf1 include:_spf.firebasemail.com ~all

   TXT    stg.dashboard.mesha.sg
          firebase=goatos-stg

   CNAME  firebase1._domainkey.stg.dashboard.mesha.sg
          mail-stg-dashboard-mesha-sg.dkim1._domainkey.firebasemail.com.

   CNAME  firebase2._domainkey.stg.dashboard.mesha.sg
          mail-stg-dashboard-mesha-sg.dkim2._domainkey.firebasemail.com.
   ```

   Keep both DKIM `CNAME` records **DNS-only**. Do not continue past the
   Cloudflare proxy warning for DKIM records.

10. Waited for authoritative and public DNS to show the full record set, then
    applied the custom domain in Firebase:

    ```bash
    for resolver in piotr.ns.cloudflare.com 1.1.1.1 8.8.8.8 9.9.9.9; do
      dig +short TXT stg.dashboard.mesha.sg @"${resolver}"
      dig +short CNAME firebase1._domainkey.stg.dashboard.mesha.sg @"${resolver}"
      dig +short CNAME firebase2._domainkey.stg.dashboard.mesha.sg @"${resolver}"
    done
    ```

    Verified config result:

    ```text
    Firebase custom email domain: stg.dashboard.mesha.sg
    Firebase custom email domain use: true
    Password reset sender local:   noreply
    Expected From domain:          stg.dashboard.mesha.sg
    ```

11. Generated a reset link with `returnOobLink: true` to test the link host
    without sending email:

    ```text
    generated link host: goatos-stg.firebaseapp.com
    generated link path: /__/auth/action
    ```

    Also tested `linkDomain: stg.dashboard.mesha.sg`; Firebase rejected it:

    ```text
    INVALID_HOSTING_LINK_DOMAIN
    ```

    Outcome: the staging sender domain is now Mesha-owned, which should help
    Gmail deliverability, but Firebase-sent reset links still show
    `goatos-stg.firebaseapp.com`. The exact dashboard-host link still requires a
    Firebase-applied custom action URL, a Firebase Hosting-backed link domain, or
    custom Goat OS email delivery. No database seed was required.

#### Production repeat checklist

Use the same flow for production, but replace every staging value before running
commands. Do not reuse the staging project, IP, OAuth client, Firebase app, or
Cloud Run service.

```bash
ENV=prod
PROJECT=goatos-prod
REGION=asia-south1
DASHBOARD_HOST=dashboard.mesha.sg
SERVICE=goatos-admin-web-prod
IMAGE_REPO="asia-south1-docker.pkg.dev/${PROJECT}/goatos/admin-web"
FIREBASE_DEFAULT_ACTION_URL="https://${PROJECT}.firebaseapp.com/__/auth/action"
CUSTOM_ACTION_URL="https://${DASHBOARD_HOST}/__/auth/action"
CONTINUE_URL="https://${DASHBOARD_HOST}/login"
```

1. Reauthenticate with browser flow whenever Google tooling asks for refresh:

   ```bash
   gcloud auth login ravi@mesha.sg
   firebase login --reauth --no-localhost
   ```

2. Verify the mutation target before touching prod:

   ```bash
   REPO_ROOT="$(git rev-parse --show-toplevel)"
   gcloud config set project "${PROJECT}"
   gcloud auth list --filter=status:ACTIVE --format='value(account)'
   gcloud config get-value project
   gcloud projects get-ancestors "${PROJECT}" --format='table(ID,TYPE)'
   gcloud organizations list --filter='DISPLAY_NAME=vgoats.com'
   git -C "${REPO_ROOT}" remote -v
   git -C "${REPO_ROOT}" status --short --branch
   ```

   Stop if the active account is not `ravi@mesha.sg`, the org is not
   `vgoats.com`, the project is not the prod Goat OS project, or the repo is not
   `vgoats/goatos`.

3. Confirm the prod dashboard host is ready before setting canonical redirects
   or Firebase templates:

   ```bash
   dig +short "${DASHBOARD_HOST}" A
   curl -fsSI "https://${DASHBOARD_HOST}/login"
   ```

   The host must also be present in Firebase/Auth Platform authorized domains
   and in the Google OAuth web client's authorized JavaScript origins. See
   `docs/runbooks/deployment.md` -> "Dashboard hostnames and DNS".

4. Build and deploy admin-web:

   ```bash
   REPO_ROOT="$(git rev-parse --show-toplevel)"
   SHA="$(git -C "${REPO_ROOT}" rev-parse --short=12 HEAD)"
   IMAGE="${IMAGE_REPO}:${SHA}-reset-auth-$(date -u +%Y%m%d%H%M%S)"

   docker buildx build \
     --platform linux/amd64 \
     -f apps/admin-web/Dockerfile \
     -t "${IMAGE}" \
     --push \
     .

   gcloud run deploy "${SERVICE}" \
     --image="${IMAGE}" \
     --region="${REGION}" \
     --project="${PROJECT}" \
     --quiet
   ```

5. Smoke the deployed prod handler before sending any reset email:

   ```bash
   curl -sS -o /dev/null -w '%{http_code} %{url_effective}\n' \
     "https://${DASHBOARD_HOST}/__/auth/action?mode=resetPassword&oobCode=dummy"

   gcloud run services describe "${SERVICE}" \
     --region="${REGION}" \
     --project="${PROJECT}" \
     --format='value(status.latestReadyRevisionName,spec.template.spec.containers[0].image,status.url)'
   ```

   The handler should return `200`; the image must match the one just deployed.

6. Configure the Firebase/Auth Platform custom email sender domain:

   ```text
   Recommended sender domain: ${DASHBOARD_HOST}
   Expected sender after apply: noreply@${DASHBOARD_HOST}
   ```

   In Firebase Console -> Authentication -> Templates -> Reset password, add the
   custom domain and copy the exact DNS records Firebase provides into
   Cloudflare. The TXT records can coexist with the dashboard A record. The DKIM
   CNAME records must be **DNS-only**; if Cloudflare asks whether to enable proxy
   status for a DKIM record, click **Cancel**.

   Wait until authoritative and public DNS resolvers return the full Firebase
   record set, then click **Apply custom domain** in Firebase.

7. Verify the Firebase sender-domain config:

   ```bash
   TOKEN="$(gcloud auth print-access-token)"
   curl -fsS \
     -H "Authorization: Bearer ${TOKEN}" \
     -H "x-goog-user-project: ${PROJECT}" \
     "https://identitytoolkit.googleapis.com/admin/v2/projects/${PROJECT}/config" \
     | jq '.notification.sendEmail'
   ```

   Also send or generate one controlled reset link for an internal mailbox and
   inspect both the sender and link host.

8. Fix the visible reset-link host before promising dashboard links:

   ```text
   Desired action URL: ${CUSTOM_ACTION_URL}
   Continue URL:       ${CONTINUE_URL}
   Current fallback:   ${FIREBASE_DEFAULT_ACTION_URL}
   ```

   If Firebase still returns `${FIREBASE_DEFAULT_ACTION_URL}`, or a template
   update fails with `EMAIL_TEMPLATE_UPDATE_NOT_ALLOWED`, choose one of these
   before broad production use:

   1. Open a Firebase support escalation to apply `${CUSTOM_ACTION_URL}`.
   2. Configure a Firebase Hosting custom domain for auth links and rewrite
      `/__/auth/action` to admin-web, then pass `linkDomain` from the clients.
   3. Send password-reset mail from Goat OS through a Mesha mail provider after
      generating the OOB code/link server-side and constructing
      `${CUSTOM_ACTION_URL}`.

   Custom sender-domain DNS fixes the sender/reputation side. It does not
   guarantee that `%LINK%` changes to `dashboard.mesha.sg`. This is not a
   Postgres seed.

The same five emails must also exist as Goat OS pending email grants with
tenant-scoped `ceo_internal` RBAC. That platform-owner cohort must have every
built visible module available, including `admin.people`. Do not seed these
accounts as department-scoped vaccination/admin operators.

Field operators are a separate provisioning lane from founder/CXO users. After
an HRMS/vaccination seed that creates vaccination operators, each operator who
can execute Android work must receive a distinct Firebase/Auth email-password
account bound to that operator's own email from the seed contract. Do not create
one shared operator login, do not reuse one password across multiple operators,
and do not ask operators to use founder/CXO accounts on Android. Generate a
unique temporary password per operator or send each operator an individual reset
flow. Never commit plaintext operator passwords into fixtures, runbooks, logs,
or screenshots.

The secure provisioning model is:

1. Keep only the approved founder/CXO emails in `auth_pending_email_grants`.
2. On SSO, the backend validates the Firebase token and normalized email.
3. If the email matches an active pending grant, the backend atomically creates
   both:
   - `user_scope_grants` with role `ceo_internal`, `scope_type='tenant'`.
   - one active `workforce_members` profile linked to the same `user_id`.
4. Android bootstrap must not depend on `user_scope_grants` alone. A claimed
   CEO/CXO account without an active `workforce_members.user_id` row is an
   invalid seed/provisioning state and will fail `/app/bootstrap`.
5. Never grant CEO/CXO from the mobile app. Mobile presents Firebase identity;
   backend seed/config decides the grant.

```bash
GOATOS_ENV=stg DATABASE_URL="$DATABASE_URL" go run ./backend/cmd/seed-dev-email-grants \
  -tenant-id 00000000-0000-4000-8000-000000000001 \
  -role ceo_internal \
  -scope-type tenant \
  -scope-id 00000000-0000-4000-8000-000000000001 \
  -email ravi@mesha.sg \
  -email manohark@mesha.sg \
  -email manju@mesha.sg \
  -email abhishek@mesha.sg \
  -email aryaman@mesha.sg
```

Post-seed / post-login verification:

```sql
SELECT p.normalized_email,
       p.role,
       p.status AS pending_status,
       p.last_claimed_user_id IS NOT NULL AS claimed,
       g.grant_id IS NOT NULL AS has_active_grant,
       m.workforce_member_id IS NOT NULL AS has_active_workforce_profile
FROM auth_pending_email_grants p
LEFT JOIN user_scope_grants g
  ON g.tenant_id = p.tenant_id
 AND g.user_id = p.last_claimed_user_id
 AND g.role = p.role
 AND g.scope_type = p.scope_type
 AND g.scope_id = p.scope_id
 AND g.status = 'active'
LEFT JOIN workforce_members m
  ON m.tenant_id = p.tenant_id
 AND m.user_id = p.last_claimed_user_id
 AND m.status = 'active'
WHERE p.normalized_email IN (
  'ravi@mesha.sg',
  'manohark@mesha.sg',
  'manju@mesha.sg',
  'abhishek@mesha.sg',
  'aryaman@mesha.sg'
)
ORDER BY p.normalized_email;
```

For localhost Firebase testing, run the admin web app in the shared-env mode,
not through `dev:local`. `dev:local` intentionally enables the local bearer
shortcut and hides Firebase sign-in. Example:

```bash
export GOATOS_FIREBASE_WEB_CONFIG="$(
  gcloud secrets versions access latest \
    --project=goatos-stg \
    --secret=goatos-stg-firebase-web-config
)"
export GOATOS_ENV=stg
export GOATOS_API_BASE_URL=https://goatos-api-stg-awtrpmn4za-el.a.run.app
export GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001
export GOATOS_GOOGLE_SIGN_IN_CLIENT_ID=514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com
npm --prefix apps/admin-web run dev -- -H 127.0.0.1 -p 3311
```

`localhost` is an authorized Firebase/Auth Platform domain for stg, so
email/password and password reset can be tested locally with the same approved
emails after those users set their password from the reset email.

Custom admin-web hosts must be registered in Firebase/Auth Platform authorized
domains. The same host origin must also be registered on the Google OAuth web
client as an authorized JavaScript origin because Google Identity Services runs
in the browser.
For `goatos-stg`, the Google Auth Platform web client is
`514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com`,
with origins for `https://stg.dashboard.mesha.sg`, the raw stg Cloud Run hosts,
and local ports `3000`, `3300`, and `3311`. The Identity Platform Google
provider must use the same client ID and secret. The `goatos-stg` OAuth app is
External / In production so approved dashboard users do not need separate
Google test-user entries; Firebase users and Goat OS DB grants still enforce
who can enter the dashboard.
Set `GOATOS_CANONICAL_DASHBOARD_HOST` on admin-web once a custom host is live so
raw Cloud Run dashboard URLs redirect to the registered OAuth host instead of
creating a second sign-in origin.

Firebase ID tokens use issuer `https://securetoken.google.com/<project-id>`,
audience `<project-id>`, and Google's SecureToken JWKS endpoint. Firebase UIDs
are external IdP subjects, not Goat OS UUIDs; the backend maps a non-UUID token
subject to a stable internal actor UUID before checking `user_scope_grants`.
Admin-web forwards `X-GoatOS-Tenant-ID` from `GOATOS_TENANT_ID`; roles still
come only from active DB grant rows for that internal actor UUID and tenant.
If a verified sign-in email has an active row in `auth_pending_email_grants`,
the backend creates the matching active tenant-scope `user_scope_grants` row
before the session cookie is accepted, so pre-approved first-time users do not
need a separate manual Firebase UID lookup.
The admin-web route proxy checks only that the session cookie is present,
well-formed, and not expired before rendering protected dashboard routes. It
does not verify the cookie signature; the backend verifies the Firebase ID
token with JWKS on `/auth/session-events` and every data API request.
The Google provider's `hd=mesha.sg` value is only an account-picker hint. It is
not the access-control boundary; the backend email allowlist plus DB grants are
the boundary.

For shared or cloud dev environments, pin the session-audit tenant and keep the
route rate-limited:

```text
GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS=<tenant uuid>[,<tenant uuid>...]
GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE=120  # default; set 0 only for controlled local smoke
```

If `GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS` is set, `/auth/session-events`
rejects verified tokens whose resolved tenant context is outside that list.

Secret Manager containers may hold the eventual issuer, audience, JWKS URL,
Firebase web config, and admin-web app bearer values, but secret values are
populated out-of-band in a later approved phase. Do not put token values,
Firebase config payloads, API keys, JWKS material, or app bearer tokens in
Terraform variables, plan files, state, or repo docs.
