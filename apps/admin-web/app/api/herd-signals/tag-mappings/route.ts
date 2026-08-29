import { NextResponse } from "next/server";
import { bindHerdSignalTagMapping, type HerdSignalIdentifierType } from "@/lib/api/herd-signals";
import { invalidBody, optionalString, readJsonBody, writeMappingResult } from "./_write";

export const dynamic = "force-dynamic";

const IDENTIFIER_TYPES: HerdSignalIdentifierType[] = ["animal_identifier_1", "animal_identifier_2", "temporary_tag"];

/** MAP — POST /herd-signals/tag-mappings. Binds an observed BLE tag to an animal. */
export async function POST(request: Request) {
  const body = await readJsonBody(request);
  if (!body) return invalidBody();

  const goatId = optionalString(body.goat_id);
  const tagId = optionalString(body.tag_id);
  if (!goatId || !tagId) {
    return NextResponse.json(
      { error: "goat_id and tag_id are required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }
  const identifierType = optionalString(body.identifier_type) as HerdSignalIdentifierType | undefined;

  return writeMappingResult(
    await bindHerdSignalTagMapping({
      goat_id: goatId,
      tag_id: tagId,
      tag_mac: optionalString(body.tag_mac),
      identifier_type: identifierType && IDENTIFIER_TYPES.includes(identifierType) ? identifierType : undefined,
    }),
  );
}
