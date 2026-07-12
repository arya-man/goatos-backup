import "server-only";

import { cookies } from "next/headers";
import {
  FIREBASE_ID_TOKEN_COOKIE,
  FIREBASE_REFRESH_TOKEN_COOKIE,
  isFirebaseIdTokenFresh,
} from "@/lib/auth/session-cookie";
import { exchangeRefreshTokenForIdToken } from "@/lib/auth/firebase-refresh";

export async function getFirebaseIdTokenCookie(): Promise<string | null> {
  const cookieStore = await cookies();
  const value = cookieStore.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim();
  return value || null;
}

// Resolve a usable Firebase ID token for SSR-side backend calls. Prefers the
// short-lived id-token cookie while it is still fresh; once it lapses (the old
// ~1h cliff), it falls back to the long-lived refresh-token cookie and mints a
// new ID token via the Firebase secure-token endpoint. Returns null only when
// neither path yields a valid token, in which case the caller redirects to
// /login. Note: a Server Component cannot write cookies, so a token minted here
// is used for this request only; the client session bridge re-persists a fresh
// id-token cookie once an authenticated page hydrates.
export async function resolveFirebaseIdToken(): Promise<string | null> {
  const cookieStore = await cookies();
  const idToken = cookieStore.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  if (idToken && isFirebaseIdTokenFresh(idToken)) {
    return idToken;
  }

  const refreshToken = cookieStore.get(FIREBASE_REFRESH_TOKEN_COOKIE)?.value.trim() || "";
  if (refreshToken) {
    const refreshed = await exchangeRefreshTokenForIdToken(refreshToken);
    if (refreshed) return refreshed.idToken;
  }

  // No fresh id token and no working refresh path — fall back to whatever id
  // token cookie exists (may be near expiry) so a valid-but-stale session still
  // works; otherwise signal "no session".
  return idToken || null;
}
