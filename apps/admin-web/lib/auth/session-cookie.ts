export const DASHBOARD_BASE_PATH = "";
export const INTERNAL_LOGIN_PATH = "/login";
export const LOGIN_PATH = `${DASHBOARD_BASE_PATH}${INTERNAL_LOGIN_PATH}`;
export const SESSION_ROUTE = `${DASHBOARD_BASE_PATH}/api/auth/session`;
export const FIREBASE_CONFIG_ROUTE = `${DASHBOARD_BASE_PATH}/api/auth/firebase-config`;
export const COOKIE_PATH = "/";
export const FIREBASE_ID_TOKEN_COOKIE = "goatos_firebase_id_token";
export const FIREBASE_ID_TOKEN_MAX_AGE_SECONDS = 60 * 60;

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
