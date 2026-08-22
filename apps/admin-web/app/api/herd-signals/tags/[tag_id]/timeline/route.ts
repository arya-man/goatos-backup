import { NextResponse } from "next/server";
import { getHerdSignalsTimeline } from "@/features/herd-signals/api";

export const dynamic = "force-dynamic";

/** Authenticated same-origin timeline read for the client-local Herd Signals drawer / full-screen history. */
export async function GET(request: Request, { params }: { params: Promise<{ tag_id: string }> }) {
  const { tag_id } = await params;
  const url = new URL(request.url);
  const from = url.searchParams.get("from") ?? undefined;
  const to = url.searchParams.get("to") ?? undefined;
  const bucketSecondsRaw = url.searchParams.get("bucket_seconds");
  const bucketSeconds = bucketSecondsRaw ? Number(bucketSecondsRaw) : undefined;

  const result = await getHerdSignalsTimeline({ tagId: tag_id, from, to, bucketSeconds });
  if (!result.ok) {
    return NextResponse.json({ error: result.error.message }, { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } });
  }
  return NextResponse.json(result.data, { headers: { "Cache-Control": "no-store" } });
}
