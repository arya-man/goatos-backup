"use server";

// Counts Breakdown -> correct a wrongly recorded breed or sex on ONE census row.
//
// The sibling of shed-stage-actions.ts, and deliberately a separate write. That one retags a whole
// PEN because a cohort tag belongs to the pen; this one touches only the row's animals because
// breed and sex belong to the animal, and one pen legitimately holds several of each.
//
// Both actions return the API result rather than redirecting: the flow is check-then-apply inside a
// popover, and a redirect after preview would throw the answer away.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import {
  commitCorrectCensusSlice,
  previewCorrectCensusSlice,
  type ApiResult,
  type CensusSliceCorrectionPreviewResponse,
  type CensusSliceCorrectionResponse,
  type CorrectCensusSliceRequest,
} from "@/lib/api/server";

const BREAKDOWN_PATH = "/counts/breakdown";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function trimmed(value: unknown, key: string, maxLength: number, required: boolean): string {
  if (value == null) {
    if (required) throw new Error(`${key} is required`);
    return "";
  }
  if (typeof value !== "string") throw new Error(`${key} is required`);
  const out = value.trim();
  if (required && out.length === 0) throw new Error(`${key} is required`);
  if (out.length > maxLength) throw new Error(`${key} is out of range`);
  return out;
}

// The server action re-validates everything the backend does, because a server action is a PUBLIC
// entry point: it is reachable without going through the component that renders the popover.
function validate(body: unknown): CorrectCensusSliceRequest {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new Error("request body is required");
  }
  const candidate = body as Record<string, unknown>;
  const shedId = trimmed(candidate.shed_id, "shed_id", 80, true);
  if (!UUID_RE.test(shedId)) throw new Error("shed_id must be a uuid");

  const partitionLabel = trimmed(candidate.partition_label, "partition_label", 80, false);
  const field = trimmed(candidate.field, "field", 16, true).toLowerCase();
  if (field !== "breed" && field !== "sex") throw new Error("field must be breed or sex");
  const sex = trimmed(candidate.sex, "sex", 16, true);
  if (sex !== "female" && sex !== "male") throw new Error("sex must be female or male");
  const value = trimmed(candidate.value, "value", 120, true);
  if (field === "sex" && value !== "female" && value !== "male") {
    throw new Error("sex must be female or male");
  }
  const reason = trimmed(candidate.reason, "reason", 500, false);

  return {
    shed_id: shedId,
    ...(partitionLabel ? { partition_label: partitionLabel } : {}),
    management_stage: trimmed(candidate.management_stage, "management_stage", 80, true),
    // Breed is legitimately empty: the census renders a blank breed as its own row.
    breed: trimmed(candidate.breed, "breed", 120, false),
    sex,
    field,
    value,
    ...(reason ? { reason } : {}),
  };
}

export async function previewCensusCorrectionAction(
  body: CorrectCensusSliceRequest,
): Promise<ApiResult<CensusSliceCorrectionPreviewResponse>> {
  return previewCorrectCensusSlice(validate(body));
}

// The idempotency key is minted by the CALLER when a preview is accepted and held across retries,
// so a double-click or a retried network failure replays the first correction instead of writing a
// second audit row for the same decision.
export async function commitCensusCorrectionAction(
  body: CorrectCensusSliceRequest,
  idempotencyKey: string,
): Promise<ApiResult<CensusSliceCorrectionResponse>> {
  const key = typeof idempotencyKey === "string" && idempotencyKey.trim() ? idempotencyKey.trim() : randomUUID();
  const result = await commitCorrectCensusSlice(validate(body), key);
  if (result.ok) {
    // The corrected row moves to a different breed/sex bucket, so the census grain rows and the
    // charts computed from them are stale by construction.
    revalidatePath(BREAKDOWN_PATH);
  }
  return result;
}
