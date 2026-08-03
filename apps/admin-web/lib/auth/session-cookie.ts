export const DASHBOARD_BASE_PATH = "";
export const INTERNAL_LOGIN_PATH = "/login";
export const LOGIN_PATH = `${DASHBOARD_BASE_PATH}${INTERNAL_LOGIN_PATH}`;
export const SESSION_ROUTE = `${DASHBOARD_BASE_PATH}/api/auth/session`;
export const FIREBASE_CONFIG_ROUTE = `${DASHBOARD_BASE_PATH}/api/auth/firebase-config`;
export const GOOGLE_REDIRECT_ROUTE = `${DASHBOARD_BASE_PATH}/api/auth/google-redirect`;
export const COOKIE_PATH = "/";
export const FIREBASE_ID_TOKEN_COOKIE = "goatos_firebase_id_token";
export const FIREBASE_ID_TOKEN_MAX_AGE_SECONDS = 60 * 60;
export const GOOGLE_REDIRECT_CREDENTIAL_COOKIE = "goatos_google_redirect_credential";
export const GOOGLE_REDIRECT_CREDENTIAL_MAX_AGE_SECONDS = 60;

// Long-lived Firebase refresh token. Persisted httpOnly so SSR can mint a fresh
// ID token after the short-lived id-token cookie lapses — this is what removes
// the ~1h "always bounced to /login" cliff. Firebase refresh tokens stay valid
// until revoked; we cap the cookie at 14 days and let the client re-issue it.
export const FIREBASE_REFRESH_TOKEN_COOKIE = "goatos_firebase_refresh_token";
export const FIREBASE_REFRESH_TOKEN_MAX_AGE_SECONDS = 60 * 60 * 24 * 14;

// Refresh the SSR ID token proactively once it is within this window of expiry,
// so a request never forwards an about-to-expire Bearer to the backend.
export const FIREBASE_ID_TOKEN_REFRESH_SKEW_SECONDS = 5 * 60;

export function secondsUntilFirebaseIdTokenExpiry(idToken: string, nowMs = Date.now()): number | null {
  const payload = decodeJwtPayload(idToken);
  const exp = typeof payload?.exp === "number" ? payload.exp : null;
  if (!exp) return null;
  return Math.floor(exp - nowMs / 1000);
}

export function isFirebaseIdTokenFresh(idToken: string, nowMs = Date.now()): boolean {
  const secondsLeft = secondsUntilFirebaseIdTokenExpiry(idToken, nowMs);
  return secondsLeft !== null && secondsLeft > FIREBASE_ID_TOKEN_REFRESH_SKEW_SECONDS;
}

export function maxAgeForFirebaseIdToken(idToken: string, nowMs = Date.now()): number | null {
  const payload = decodeJwtPayload(idToken);
  const exp = typeof payload?.exp === "number" ? payload.exp : null;
  if (!exp) return null;
  const secondsLeft = Math.floor(exp - nowMs / 1000);
  if (secondsLeft <= 0) return null;
  return Math.min(secondsLeft, FIREBASE_ID_TOKEN_MAX_AGE_SECONDS);
}

export function isLikelyJwt(value: string): boolean {
  return value.split(".").length === 3;
}

// Firebase user id (uid) carried in the `sub` claim of an ID token. Used to bind
// the long-lived refresh token to the same user the ID token authenticated, so a
// caller cannot pair account A's ID token with account B's refresh token.
export function firebaseUidFromToken(token: string): string | null {
  const payload = decodeJwtPayload(token);
  const sub = typeof payload?.sub === "string" ? payload.sub.trim() : "";
  return sub || null;
}

function decodeJwtPayload(token: string): Record<string, unknown> | null {
  if (!isLikelyJwt(token)) return null;
  const [, payload] = token.split(".");
  try {
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    return JSON.parse(atob(padded)) as Record<string, unknown>;
  } catch {
    return null;
  }
}
