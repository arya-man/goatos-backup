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

Admin-web login uses Google Identity Services directly to receive a Google ID
token, then signs into Firebase Auth with that credential. The direct Google
client is initialized with account auto-select disabled and the `mesha.sg`
hosted-domain hint. The Cloud Run admin-web origins must remain registered on
the Google OAuth web client.

Firebase ID tokens use issuer `https://securetoken.google.com/goatos-dev`,
audience `goatos-dev`, and Google's SecureToken JWKS endpoint. Firebase UIDs are
external IdP subjects, not Goat OS UUIDs; the backend maps a non-UUID token
subject to a stable internal actor UUID before checking `user_scope_grants`.
Admin-web forwards `X-GoatOS-Tenant-ID` from `GOATOS_TENANT_ID`; roles still
come only from active DB grant rows for that internal actor UUID and tenant.

Secret Manager containers may hold the eventual issuer, audience, JWKS URL,
Firebase web config, and admin-web app bearer values, but secret values are
populated out-of-band in a later approved phase. Do not put token values,
Firebase config payloads, API keys, JWKS material, or app bearer tokens in
Terraform variables, plan files, state, or repo docs.
