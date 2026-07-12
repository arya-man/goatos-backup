import "server-only";

import type { FirebaseOptions } from "firebase/app";

// Firebase secure-token endpoint: exchange a long-lived refresh token for a
// fresh ID token. This is what lets SSR keep a valid backend Bearer for weeks
// (the refresh token's lifetime) instead of being capped at the ~1h ID-token
// TTL. https://firebase.google.com/docs/reference/rest/auth#section-refresh-token
const SECURE_TOKEN_URL = "https://securetoken.googleapis.com/v1/token";

export type RefreshedFirebaseToken = {
  idToken: string;
  refreshToken: string;
  expiresInSeconds: number;
};

export async function exchangeRefreshTokenForIdToken(
  refreshToken: string,
): Promise<RefreshedFirebaseToken | null> {
  const apiKey = firebaseWebApiKey();
  const token = refreshToken.trim();
  if (!apiKey || !token) return null;

  let response: Response;
  try {
    response = await fetch(`${SECURE_TOKEN_URL}?key=${encodeURIComponent(apiKey)}`, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ grant_type: "refresh_token", refresh_token: token }).toString(),
    });
  } catch {
    return null;
  }
  if (!response.ok) return null;

  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return null;
  }
  if (!isRecord(payload)) return null;

  const idToken = typeof payload.id_token === "string" ? payload.id_token.trim() : "";
  const rotatedRefresh = typeof payload.refresh_token === "string" ? payload.refresh_token.trim() : token;
  const expiresInSeconds = parseExpiresIn(payload.expires_in);
  if (!idToken || expiresInSeconds <= 0) return null;

  return { idToken, refreshToken: rotatedRefresh || token, expiresInSeconds };
}

function firebaseWebApiKey(): string | null {
  const raw = process.env.GOATOS_FIREBASE_WEB_CONFIG?.trim();
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as FirebaseOptions;
      if (typeof parsed.apiKey === "string" && parsed.apiKey.trim()) return parsed.apiKey.trim();
    } catch {
      // fall through to the discrete env var
    }
  }
  const apiKey = process.env.NEXT_PUBLIC_FIREBASE_API_KEY?.trim();
  return apiKey || null;
}

function parseExpiresIn(value: unknown): number {
  if (typeof value === "number") return Math.floor(value);
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
