import { NextResponse } from "next/server";
import { replaceHerdSignalTagMapping, type HerdSignalIdentifierType } from "@/lib/api/herd-signals";
import { invalidBody, optionalString, readJsonBody, writeMappingResult } from "../_write";

export const dynamic = "force-dynamic";

const IDENTIFIER_TYPES: HerdSignalIdentifierType[] = ["animal_identifier_1", "animal_identifier_2", "temporary_tag"];

/**
 * REPLACE — POST /herd-signals/tag-mappings/replace.
 *
 * ONE operation, never unmap-then-map as two calls: both halves commit together or neither does,
 * so the animal is never left carrying two live smart tags or none.
 */
export async function POST(request: Request) {
  const body = await readJsonBody(request);
  if (!body) return invalidBody();

  const goatId = optionalString(body.goat_id);
  const newTagId = optionalString(body.new_tag_id);
  if (!goatId || !newTagId) {
    return NextResponse.json(
      { error: "goat_id and new_tag_id are required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }
  const identifierType = optionalString(body.identifier_type) as HerdSignalIdentifierType | undefined;

  return writeMappingResult(
    await replaceHerdSignalTagMapping({
      goat_id: goatId,
      new_tag_id: newTagId,
      new_tag_mac: optionalString(body.new_tag_mac),
      identifier_type: identifierType && IDENTIFIER_TYPES.includes(identifierType) ? identifierType : undefined,
    }),
  );
}
