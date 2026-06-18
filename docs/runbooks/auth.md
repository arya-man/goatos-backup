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
```

Authorization still comes from active `user_scope_grants` rows in Goat OS; token
claims authenticate the subject but do not grant product roles by themselves.

## goatos-dev IdP

`goatos-dev` uses Google Identity Platform / Firebase Auth as the real IdP for
JWKS verification. Firebase is auth only for Goat OS dev bring-up: do not use
Firebase Hosting or Firebase App Hosting, and do not introduce an external OIDC
provider or static dev JWKS.

Admin-web login uses the Firebase Web SDK `signInWithPopup` path with
`GoogleAuthProvider`. The provider must set `prompt=select_account` and
`hd=mesha.sg` so shared browsers show the Google account chooser instead of
silently reusing a personal default account. Do not reintroduce the Google
Identity Services rendered button or One Tap path for this internal dashboard;
those flows are easy to misconfigure for custom domains and can auto-select the
wrong browser account.

Custom admin-web hosts must be registered in Firebase/Auth Platform authorized
domains. If any direct Google Identity Services or browser OAuth flow is used
again, the same host origin must also be registered on the Google OAuth web
client as an authorized JavaScript origin.

Firebase ID tokens use issuer `https://securetoken.google.com/goatos-dev`,
audience `goatos-dev`, and Google's SecureToken JWKS endpoint. Firebase UIDs are
external IdP subjects, not Goat OS UUIDs; the backend maps a non-UUID token
subject to a stable internal actor UUID before checking `user_scope_grants`.
Admin-web forwards `X-GoatOS-Tenant-ID` from `GOATOS_TENANT_ID`; roles still
come only from active DB grant rows for that internal actor UUID and tenant.
The admin-web route proxy checks only that the session cookie is present,
well-formed, and not expired before rendering protected dashboard routes. It
does not verify the cookie signature; the backend verifies the Firebase ID
token with JWKS on `/auth/session-events` and every data API request.

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
