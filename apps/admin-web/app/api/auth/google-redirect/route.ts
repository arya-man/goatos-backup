import { NextRequest, NextResponse } from "next/server";
import {
  COOKIE_PATH,
  GOOGLE_REDIRECT_CREDENTIAL_COOKIE,
  GOOGLE_REDIRECT_CREDENTIAL_MAX_AGE_SECONDS,
  LOGIN_PATH,
  isLikelyJwt,
} from "@/lib/auth/session-cookie";

export const dynamic = "force-dynamic";

export async function POST(request: NextRequest) {
  const form = await request.formData();
  const credential = formValue(form, "credential");
  const state = formValue(form, "state");
  const csrfBody = formValue(form, "g_csrf_token");
  const csrfCookie = request.cookies.get("g_csrf_token")?.value.trim() ?? "";
  const nextPath = safeNextPath(state);
  const redirectUrl = new URL(LOGIN_PATH, request.url);
  redirectUrl.searchParams.set("next", nextPath);

  if (!csrfBody || csrfBody !== csrfCookie) {
    redirectUrl.searchParams.set("google_error", "csrf");
    return NextResponse.redirect(redirectUrl, 303);
  }

  if (!credential || !isLikelyJwt(credential)) {
    redirectUrl.searchParams.set("google_error", "missing_credential");
    return NextResponse.redirect(redirectUrl, 303);
  }

  redirectUrl.searchParams.set("google_redirect", "1");
  const response = NextResponse.redirect(redirectUrl, 303);
  response.cookies.set(GOOGLE_REDIRECT_CREDENTIAL_COOKIE, credential, {
    httpOnly: true,
    secure: request.nextUrl.protocol === "https:",
    sameSite: "lax",
    path: COOKIE_PATH,
    maxAge: GOOGLE_REDIRECT_CREDENTIAL_MAX_AGE_SECONDS,
  });
  return response;
}

export async function GET(request: NextRequest) {
  const credential = request.cookies.get(GOOGLE_REDIRECT_CREDENTIAL_COOKIE)?.value.trim() ?? "";
  const response = NextResponse.json(
    credential && isLikelyJwt(credential) ? { credential } : { error: "google_redirect_credential_missing" },
    { status: credential && isLikelyJwt(credential) ? 200 : 404, headers: { "Cache-Control": "no-store" } },
  );
  response.cookies.set(GOOGLE_REDIRECT_CREDENTIAL_COOKIE, "", {
    httpOnly: true,
    secure: request.nextUrl.protocol === "https:",
    sameSite: "lax",
    path: COOKIE_PATH,
    maxAge: 0,
  });
  return response;
}

function formValue(form: FormData, key: string): string {
  const value = form.get(key);
  return typeof value === "string" ? value.trim() : "";
}

function safeNextPath(value: string): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/";
  try {
    const decoded = decodeURI(value);
    if (decoded.includes("\\")) return "/";
  } catch {
    return "/";
  }
  try {
    const parsed = new URL(value, "https://dev.dashboard.mesha.sg");
    if (parsed.origin !== "https://dev.dashboard.mesha.sg") return "/";
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return "/";
  }
}
