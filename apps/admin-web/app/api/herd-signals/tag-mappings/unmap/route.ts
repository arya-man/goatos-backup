import { NextResponse } from "next/server";
import { unmapHerdSignalTagMapping } from "@/lib/api/herd-signals";
import { invalidBody, optionalString, readJsonBody, writeMappingResult } from "../_write";

export const dynamic = "force-dynamic";

/**
 * UNMAP — POST /herd-signals/tag-mappings/unmap. Releases a binding.
 *
 * Nothing is deleted: the tag keeps broadcasting and its stored history stays intact, it simply
 * stops being attributed to an animal. Its monitoring period ends there.
 */
export async function POST(request: Request) {
  const body = await readJsonBody(request);
  if (!body) return invalidBody();

  const tagId = optionalString(body.tag_id);
  if (!tagId) {
    return NextResponse.json(
      { error: "tag_id is required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  return writeMappingResult(
    await unmapHerdSignalTagMapping({ tag_id: tagId, tag_mac: optionalString(body.tag_mac) }),
  );
}
