import { NextResponse } from "next/server";
import { leadershipTaskAttachmentDownloadURL } from "@/lib/api/server";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ taskId: string; proofId: string }> },
) {
  const { taskId, proofId } = await params;
  const result = await leadershipTaskAttachmentDownloadURL(taskId, proofId);
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.code ?? result.error.kind },
      { status: 404 },
    );
  }
  return NextResponse.redirect(result.data.download_url);
}
