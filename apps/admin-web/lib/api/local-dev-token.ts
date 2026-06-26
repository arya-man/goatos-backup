import { createHmac } from "node:crypto";

// Local-dev self-minted bearer token — PERMANENT fix for the recurring
// `invalid_bearer_token` ("Google sign-in session is missing or expired") on the local :3300 dev server.
//
// Root cause: `npm run dev:local` mints ONE token at boot (TTL capped at 24h by the backend). The Next
// server pins that token in `GOATOS_BEARER_TOKEN` for its whole lifetime, so once it expires every SSR
// fetch 401s until someone restarts the server. Instead of depending on a boot-time token, this signs a
// FRESH short-lived HS256 token per request (cached, re-minted a few minutes before expiry), matching
// backend/internal/platform/auth.MintHS256Token exactly. The token can therefore never go stale,
// regardless of how long the dev server has been running — no restart, ever.
//
// STRICTLY local-only. It only runs when GOATOS_ENV === "local" AND GOATOS_AUTH_MODE === "bearer" AND the
// local HS256 dev secret is present. In staging/production those are unset (Firebase session mode, no
// HS256 secret), so this returns null and the normal Firebase path is used — it is never a production
// auth path.

const DEFAULT_LOCAL_USER_ID = "90000000-0000-4000-8000-000000000101";
const TOKEN_TTL_SECONDS = 30 * 60; // 30 min — well under the backend's 24h max-TTL ceiling
const REFRESH_BEFORE_SECONDS = 5 * 60; // re-mint when fewer than 5 min remain
const MIN_SECRET_BYTES = 32; // backend rejects weaker HS256 secrets
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function base64url(input: Buffer | string): string {
  return Buffer.from(input).toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

let cached: { token: string; expSeconds: number } | null = null;

// Returns a freshly-signed local dev bearer token, or null when not in local bearer mode (or the dev
// secret / required claims are missing) — in which case the caller falls back to the Firebase session.
export function mintLocalDevBearerToken(): string | null {
  if (process.env.GOATOS_ENV !== "local" || process.env.GOATOS_AUTH_MODE !== "bearer") return null;

  const secret = process.env.GOATOS_AUTH_HS256_SECRET ?? "";
  const issuer = (process.env.GOATOS_AUTH_ISSUER ?? "").trim();
  const audience = (process.env.GOATOS_AUTH_AUDIENCE ?? "").trim();
  const tenantId = (process.env.GOATOS_TENANT_ID ?? "").trim();
  const subject = (process.env.GOATOS_LOCAL_USER_ID ?? DEFAULT_LOCAL_USER_ID).trim();

  // Match the backend's minting preconditions: a strong-enough secret, issuer + audience, and UUID
  // sub/tenant. If any is missing we cannot mint a valid token — fall back to the Firebase path.
  if (secret.length < MIN_SECRET_BYTES || !issuer || !audience || !UUID_RE.test(subject) || !UUID_RE.test(tenantId)) {
    return null;
  }

  const nowSeconds = Math.floor(Date.now() / 1000);
  if (cached && cached.expSeconds - nowSeconds > REFRESH_BEFORE_SECONDS) {
    return cached.token;
  }

  const expSeconds = nowSeconds + TOKEN_TTL_SECONDS;
  const header = { alg: "HS256", typ: "JWT" };
  const payload = {
    iss: issuer,
    aud: audience,
    sub: subject,
    tenant_id: tenantId,
    exp: expSeconds,
    nbf: nowSeconds - 60, // mirrors MintHS256Token's now-1min not-before
  };
  const signingInput = `${base64url(JSON.stringify(header))}.${base64url(JSON.stringify(payload))}`;
  const signature = base64url(createHmac("sha256", secret).update(signingInput).digest());
  const token = `${signingInput}.${signature}`;
  cached = { token, expSeconds };
  return token;
}
