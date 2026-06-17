import { NextResponse, type NextRequest } from "next/server";
import { DASHBOARD_BASE_PATH, FIREBASE_ID_TOKEN_COOKIE, INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";

const publicDashboardPrefixes = ["/api/auth", "/_next", "/apple-icon.png", "/favicon.ico", "/icon.png", "/login"];

export function proxy(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const dashboardPath = pathname.startsWith(DASHBOARD_BASE_PATH)
    ? pathname.slice(DASHBOARD_BASE_PATH.length) || "/"
    : pathname;
  if (publicDashboardPrefixes.some((prefix) => dashboardPath === prefix || dashboardPath.startsWith(`${prefix}/`))) {
    return NextResponse.next();
  }
  if (request.cookies.has(FIREBASE_ID_TOKEN_COOKIE) || hasLocalBearerFallback()) {
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
