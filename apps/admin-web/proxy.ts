import { NextResponse, type NextRequest } from "next/server";
import {
  FIREBASE_ID_TOKEN_COOKIE,
  LOGIN_PATH,
  maxAgeForFirebaseIdToken,
} from "@/lib/auth/session-cookie";

const publicDashboardPrefixes = [
  "/__/auth/action",
  "/api/auth",
  "/_next",
  "/apple-icon.png",
  "/auth/action",
  "/favicon.ico",
  "/icon.png",
  "/login",
];

export function proxy(request: NextRequest) {
  const canonicalRedirect = canonicalHostRedirect(request);
  if (canonicalRedirect) {
    return canonicalRedirect;
  }

  const pathname = request.nextUrl.pathname;
  const sessionIsValid =
    maxAgeForFirebaseIdToken(request.cookies.get(FIREBASE_ID_TOKEN_COOKIE)?.value.trim() || "") !== null ||
    hasLocalBearerFallback();
  if (isLoginPath(pathname) && sessionIsValid) {
    return NextResponse.redirect(loginNextUrl(request));
  }
  if (publicDashboardPrefixes.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))) {
    return NextResponse.next();
  }
  if (sessionIsValid) {
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

function isLoginPath(pathname: string): boolean {
  return pathname === LOGIN_PATH || pathname.startsWith(`${LOGIN_PATH}/`);
}

function loginNextUrl(request: NextRequest): URL {
  const requestedNext = safeInternalPath(request.nextUrl.searchParams.get("next"));
  return new URL(requestedNext, request.nextUrl.origin);
}

function safeInternalPath(value: string | null): string {
  if (!value?.startsWith("/") || value.startsWith("//")) {
    return "/";
  }
  return value;
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
  // A loopback host is the Next server talking to ITSELF: when a Server Action calls redirect(),
  // Next fetches the redirect target from `__NEXT_PRIVATE_ORIGIN` (http://127.0.0.1:<port> on Cloud
  // Run) to stream the destination page back in the action response. Node's fetch overrides the
  // Host header from that URL, so the internal request arrives as `127.0.0.1:8080`. Deflecting it
  // to the public host would send it back out through the load balancer, and Node's fetch DROPS the
  // Cookie header when following a cross-origin redirect — the request then arrives sessionless,
  // bounces to /login, and the user sees a logout/login flash after every approve (STG incident
  // 2026-08-18). The self-fetch must be served in place, never re-canonicalized.
  if (isLoopbackHost(requestHost)) {
    return null;
  }
  const redirectUrl = request.nextUrl.clone();
  redirectUrl.protocol = "https:";
  redirectUrl.hostname = canonicalHost;
  redirectUrl.port = "";
  return NextResponse.redirect(redirectUrl, 308);
}

function isLoopbackHost(host: string): boolean {
  const bare = host.replace(/:\d+$/, "");
  return bare === "127.0.0.1" || bare === "localhost" || bare === "[::1]";
}

function normalizeHost(value: string | null | undefined): string {
  const host = (value ?? "").trim().toLowerCase().replace(/\/+$/, "");
  return host.replace(/:(443|80)$/, "");
}
