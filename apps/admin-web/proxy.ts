import { NextResponse, type NextRequest } from "next/server";
import {
  DASHBOARD_BASE_PATH,
  FIREBASE_ID_TOKEN_COOKIE,
  INTERNAL_LOGIN_PATH,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";

const publicDashboardPrefixes = ["/api/auth", "/_next", "/apple-icon.png", "/favicon.ico", "/icon.png", "/login"];

export function proxy(request: NextRequest) {
  const canonicalRedirect = canonicalHostRedirect(request);
  if (canonicalRedirect) {
    return canonicalRedirect;
  }

  const pathname = request.nextUrl.pathname;
  const dashboardPath = pathname.startsWith(DASHBOARD_BASE_PATH)
    ? pathname.slice(DASHBOARD_BASE_PATH.length) || "/"
    : pathname;
  if (publicDashboardPrefixes.some((prefix) => dashboardPath === prefix || dashboardPath.startsWith(`${prefix}/`))) {
    return NextResponse.next();
  }
  const sessionCookie = request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  if (maxAgeForFirebaseIdToken(sessionCookie) !== null || hasLocalBearerFallback()) {
    return NextResponse.next();
  }
  const loginUrl = request.nextUrl.clone();
  loginUrl.pathname = INTERNAL_LOGIN_PATH;
  loginUrl.search = "";
  return NextResponse.redirect(loginUrl);
}

export const config = {
  matcher: ["/:path*"],
};

function hasLocalBearerFallback(): boolean {
  return (
    process.env.GOATOS_ENV === "local" &&
    process.env.GOATOS_AUTH_MODE === "bearer" &&
    Boolean(process.env.GOATOS_BEARER_TOKEN)
  );
}

function canonicalHostRedirect(request: NextRequest): NextResponse | null {
  const canonicalHost = normalizeHost(process.env.GOATOS_CANONICAL_DASHBOARD_HOST);
  if (!canonicalHost) {
    return null;
  }
  const requestHost = normalizeHost(request.headers.get("host") ?? request.nextUrl.host);
  if (!requestHost || requestHost === canonicalHost) {
    return null;
  }
  const redirectUrl = request.nextUrl.clone();
  redirectUrl.protocol = "https:";
  redirectUrl.host = canonicalHost;
  return NextResponse.redirect(redirectUrl, 308);
}

function normalizeHost(value: string | null | undefined): string {
  return (value ?? "").trim().toLowerCase().replace(/\/+$/, "");
}
