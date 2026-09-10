import { NextResponse } from "next/server";
import { getProofDownloadUrl } from "@/lib/api/server";

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ proof_id: string }> },
): Promise<NextResponse> {
  const { proof_id: proofId } = await params;
  // serial-await: allow getProofDownloadUrl depends on the route proof_id param.
  const url = await getProofDownloadUrl(proofId);
  if (!url) {
    return NextResponse.json({ error: "proof_media_unavailable" }, { status: 404 });
  }
  return NextResponse.redirect(url);
}
