import { NextResponse } from "next/server";
import { getHerdSignalsTimeline } from "@/lib/api/herd-signals";

export const dynamic = "force-dynamic";

// Authenticated same-origin proxy for the drawer's mini-chart and the full-screen history view.
// Both are client-local overlays (never a route navigation — see components/local-overlay-link.tsx
// and make admin-web-local-overlay-guard), so their own range/bucket switches fetch through here
// instead of forcing a Suspense remount of the whole board. Bearer-token minting stays server-only,
// same reasoning as the Goat Passport drawer's proxy route.
export async function GET(request: Request, { params }: { params: Promise<{ tagId: string }> }) {
  const { tagId } = await params;
  if (!tagId) {
    return NextResponse.json({ error: "invalid_tag_id" }, { status: 400, headers: { "Cache-Control": "no-store" } });
  }

  const url = new URL(request.url);
  const from = url.searchParams.get("from") ?? undefined;
  const to = url.searchParams.get("to") ?? undefined;
  const bucketSecondsRaw = url.searchParams.get("bucket_seconds");
  const bucketSeconds = bucketSecondsRaw ? Number.parseInt(bucketSecondsRaw, 10) : undefined;

  const result = await getHerdSignalsTimeline({
    tagId,
    from,
    to,
    bucketSeconds: bucketSeconds && Number.isFinite(bucketSeconds) ? bucketSeconds : undefined,
  });

  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  return NextResponse.json(result.data, { headers: { "Cache-Control": "no-store" } });
}
