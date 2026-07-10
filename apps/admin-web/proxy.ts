import { NextResponse, type NextRequest } from "next/server";
import {
  FIREBASE_ID_TOKEN_COOKIE,
  LOGIN_PATH,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";

const publicDashboardPrefixes = ["/api/auth", "/_next", "/apple-icon.png", "/favicon.ico", "/icon.png", "/login"];

export function proxy(request: NextRequest) {
  const canonicalRedirect = canonicalHostRedirect(request);
  if (canonicalRedirect) {
    return canonicalRedirect;
  }

  const pathname = request.nextUrl.pathname;
  if (publicDashboardPrefixes.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))) {
    return NextResponse.next();
  }
  const sessionCookie = request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "";
  if (maxAgeForFirebaseIdToken(sessionCookie) !== null || hasLocalBearerFallback()) {
    return NextResponse.next();
  }
  const loginUrl = request.nextUrl.clone();
  loginUrl.pathname = LOGIN_PATH;
  loginUrl.search = "";
  const returnPath = `${request.nextUrl.pathname}${request.nextUrl.search}`;
  if (returnPath !== LOGIN_PATH) {
    loginUrl.searchParams.set("next", returnPath);
  }
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
  redirectUrl.hostname = canonicalHost;
  redirectUrl.port = "";
  return NextResponse.redirect(redirectUrl, 308);
}

function normalizeHost(value: string | null | undefined): string {
  const host = (value ?? "").trim().toLowerCase().replace(/\/+$/, "");
  return host.replace(/:(443|80)$/, "");
}
