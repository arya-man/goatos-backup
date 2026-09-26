import { NextResponse } from "next/server";
import { getInAppNotificationFeed } from "@/lib/api/notifications-server";
import { NOTIFICATION_PAGE_LIMIT, type NotificationFeedActionResult } from "@/features/notifications";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" } as const;

/**
 * GET /api/notifications?cursor=...&limit=... -- one page of the caller's in-app notifications
 * (`limit=1` is the badge poll; the cap is the feed's own page size).
 *
 * A route handler, NOT a Server Action, on purpose: the bell reads on mount, on every route change
 * and on open, and an action POST re-renders the current page server-side -- on a page that
 * `redirect()`s the response carried the redirect and crashed the Router mid-navigation. This
 * renders nothing. Same session auth as the actions (`getServerConfig` reads the cookies), same
 * `{ ok, feed } | { ok, code }` envelope, so the client swap is one function.
 */
export async function GET(request: Request): Promise<NextResponse<NotificationFeedActionResult>> {
  const raw = new URL(request.url).searchParams.get("cursor")?.trim() ?? "";
  const cursor = raw ? raw.slice(0, 512) : undefined;
  const limitRaw = Number.parseInt(new URL(request.url).searchParams.get("limit") ?? "", 10);
  const limit = Number.isFinite(limitRaw) ? Math.max(1, Math.min(limitRaw, NOTIFICATION_PAGE_LIMIT)) : NOTIFICATION_PAGE_LIMIT;
  const result = await getInAppNotificationFeed({ cursor, limit });
  if (!result.ok) {
    return NextResponse.json(
      { ok: false, code: result.error.code ?? result.error.kind },
      { status: result.error.status ?? 500, headers: NO_STORE },
    );
  }
  return NextResponse.json({ ok: true, feed: result.data }, { headers: NO_STORE });
}
