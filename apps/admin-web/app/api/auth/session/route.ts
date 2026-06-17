import { AUTH_SESSION_USER_AGENT_HEADER, TENANT_CONTEXT_HEADER } from "@goatos/api-client/constants";
import { type NextRequest, NextResponse } from "next/server";
import {
  DASHBOARD_BASE_PATH,
  FIREBASE_ID_TOKEN_COOKIE,
  isLikelyJwt,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";
import { allowUnauditedSessionRefresh, type AuthSessionEventType } from "@/lib/auth/session-audit-policy";

export const dynamic = "force-dynamic";

export async function POST(request: NextRequest) {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid_json" }, { status: 400 });
  }

  const payload = isRecord(body) ? body : {};
  const idToken = typeof payload.idToken === "string" ? payload.idToken.trim() : "";
  const eventType = parseSessionEventType(payload.eventType, "auth.session_refresh");
  const maxAge = maxAgeForFirebaseIdToken(idToken);
  if (!idToken || maxAge === null) {
    return NextResponse.json({ error: "invalid_or_expired_id_token" }, { status: 401 });
  }

  const audit = await recordBackendAuthEvent(request, idToken, eventType);
  const existingSessionCookie = request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  const existingSessionUsable = maxAgeForFirebaseIdToken(existingSessionCookie) !== null;
  const allowUnauditedRefresh =
    !audit.ok && allowUnauditedSessionRefresh(eventType, audit.status, existingSessionUsable);
  if (!audit.ok && !allowUnauditedRefresh) {
    return NextResponse.json({ error: audit.error }, { status: audit.status });
  }
  if (allowUnauditedRefresh) {
    console.warn("auth_session_refresh_audit_unavailable", {
      audit_error: audit.error,
      audit_status: audit.status,
    });
  }

  const response = NextResponse.json({ ok: true, maxAge, audit_recorded: audit.ok });
  response.cookies.set({
    name: FIREBASE_ID_TOKEN_COOKIE,
    value: idToken,
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: DASHBOARD_BASE_PATH,
    maxAge,
  });
  return response;
}

export async function DELETE(request: NextRequest) {
  const idToken = request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  const audit = isLikelyJwt(idToken) ? await recordBackendAuthEvent(request, idToken, "auth.sign_out") : { ok: false };
  const response = NextResponse.json({ ok: true, audit_recorded: audit.ok });
  response.cookies.set({
    name: FIREBASE_ID_TOKEN_COOKIE,
    value: "",
    httpOnly: true,
    secure: true,
    sameSite: "lax",
    path: DASHBOARD_BASE_PATH,
    maxAge: 0,
  });
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
  return {
    ok: false,
    status: response.status >= 400 && response.status < 500 ? response.status : 502,
    error: "auth_audit_failed",
  };
}
