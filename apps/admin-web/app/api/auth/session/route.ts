import { AUTH_SESSION_USER_AGENT_HEADER, TENANT_CONTEXT_HEADER } from "@goatos/api-client/constants";
import { type NextRequest, NextResponse } from "next/server";
import {
  COOKIE_PATH,
  FIREBASE_ID_TOKEN_COOKIE,
  FIREBASE_REFRESH_TOKEN_COOKIE,
  FIREBASE_REFRESH_TOKEN_MAX_AGE_SECONDS,
  firebaseUidFromToken,
  isLikelyJwt,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";
import { exchangeRefreshTokenForIdToken } from "@/lib/auth/firebase-refresh";
import { refreshCookieState, resolveBoundRefreshToken } from "@/lib/auth/refresh-binding";
import { planSessionUpdate } from "@/lib/auth/session-update";

export const dynamic = "force-dynamic";

type AuthSessionEventType = "auth.sign_in" | "auth.session_refresh" | "auth.sign_out";

export async function POST(request: NextRequest) {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid_json" }, { status: 400 });
  }

  const payload = isRecord(body) ? body : {};
  const idToken = typeof payload.idToken === "string" ? payload.idToken.trim() : "";
  const refreshToken = typeof payload.refreshToken === "string" ? payload.refreshToken.trim() : "";
  const eventType = parseSessionEventType(payload.eventType, "auth.session_refresh");
  const maxAge = maxAgeForFirebaseIdToken(idToken);
  if (!idToken || maxAge === null) {
    return NextResponse.json({ error: "invalid_or_expired_id_token" }, { status: 401 });
  }

  // Bind the refresh token BEFORE recording the audit event. The refresh token —
  // the durable 14-day credential SSR mints fresh id tokens from — must be proven
  // to belong to the same user the id token authenticated; an unbound token could
  // pair account A's id token with account B's refresh token, silently turning
  // the session into B once the id token expires. A verified uid mismatch is a
  // tampered pair and must fail with NO cookies set AND NO audit event written —
  // recording a successful sign_in/refresh for a rejected pair would corrupt the
  // security/audit trail. So binding (the non-mutating rejection gate) runs
  // first; the event is recorded only once the pair is trustworthy.
  const plan = await planSessionUpdate({
    resolveBinding: () =>
      resolveBoundRefreshToken(
        firebaseUidFromToken(idToken),
        refreshToken,
        exchangeRefreshTokenForIdToken,
        firebaseUidFromToken,
      ),
    recordEvent: () => recordBackendAuthEvent(request, idToken, eventType),
  });
  if (plan.outcome !== "commit") {
    return NextResponse.json({ error: plan.error }, { status: plan.status });
  }
  const binding = plan.binding;

  const response = NextResponse.json({ ok: true, maxAge, audit_recorded: plan.eventRecorded });
  response.cookies.set({
    name: FIREBASE_ID_TOKEN_COOKIE,
    value: idToken,
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: COOKIE_PATH,
    maxAge,
  });
  // Persist the long-lived refresh token so SSR can mint fresh ID tokens after
  // the id-token cookie above expires (removes the ~1h forced-relogin cliff).
  // ALWAYS write this cookie: on `store` persist the verified, rotated token
  // bound to this user; on `skip` (exchange outage / no refresh token /
  // undecodable uid) clear it, so a prior user's refresh token cannot survive an
  // account switch and revert the session once the new id token expires.
  const refreshCookie = refreshCookieState(binding, FIREBASE_REFRESH_TOKEN_MAX_AGE_SECONDS);
  response.cookies.set({
    name: FIREBASE_REFRESH_TOKEN_COOKIE,
    value: refreshCookie.value,
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: COOKIE_PATH,
    maxAge: refreshCookie.maxAge,
  });
  return response;
}

export async function DELETE(request: NextRequest) {
  const idToken = request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  const audit = isLikelyJwt(idToken) ? await recordBackendAuthEvent(request, idToken, "auth.sign_out") : { ok: false };
  const response = NextResponse.json({ ok: true, audit_recorded: audit.ok });
  for (const name of [FIREBASE_ID_TOKEN_COOKIE, FIREBASE_REFRESH_TOKEN_COOKIE]) {
    response.cookies.set({
      name,
      value: "",
      httpOnly: true,
      secure: true,
      sameSite: "lax",
      path: COOKIE_PATH,
      maxAge: 0,
    });
  }
  return response;
}

function parseSessionEventType(value: unknown, fallback: AuthSessionEventType): AuthSessionEventType {
  return value === "auth.sign_in" || value === "auth.session_refresh" || value === "auth.sign_out" ? value : fallback;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

async function recordBackendAuthEvent(
  request: NextRequest,
  idToken: string,
  eventType: AuthSessionEventType,
): Promise<{ ok: true } | { ok: false; status: number; error: string }> {
  const baseUrl = (process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080").replace(/\/+$/, "");
  const tenantId = process.env.GOATOS_TENANT_ID?.trim();
  if (!tenantId) {
    return { ok: false, status: 503, error: "tenant_config_missing" };
  }

  let response: Response;
  try {
    response = await fetch(`${baseUrl}/auth/session-events`, {
      method: "POST",
      cache: "no-store",
      headers: {
        "Authorization": `Bearer ${idToken}`,
        "Content-Type": "application/json",
        [TENANT_CONTEXT_HEADER]: tenantId,
        [AUTH_SESSION_USER_AGENT_HEADER]: request.headers.get("user-agent") ?? "",
      },
      body: JSON.stringify({ event_type: eventType, source: "admin-web" }),
    });
  } catch {
    return { ok: false, status: 502, error: "auth_audit_unreachable" };
  }

  if (response.ok) {
    return { ok: true };
  }
  const backendError = await backendErrorCode(response);
  return {
    ok: false,
    status: response.status >= 400 && response.status < 500 ? response.status : 502,
    error: backendError ?? "auth_audit_failed",
  };
}

async function backendErrorCode(response: Response): Promise<string | null> {
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return null;
  }
  if (!isRecord(payload) || typeof payload.code !== "string") {
    return null;
  }
  return payload.code;
}
