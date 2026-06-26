"use server";

import { revalidatePath } from "next/cache";
import {
  acceptVaccinationCompletion,
  completeProofUpload,
  createProofUpload,
  rejectVaccinationCompletion,
  submitAppTask,
  uploadProofLocal,
} from "@/lib/api/server";

/**
 * Result of a vaccination server action. The operator-facing UI surfaces `error`
 * directly (alert band), so failures are visible rather than swallowed or logged-only.
 */
export type ActionResult = { ok: true } | { ok: false; error: string };

function actionError(error: unknown): ActionResult {
  return { ok: false, error: error instanceof Error ? error.message : "Unexpected error" };
}

/**
 * Submit a vaccination proof upload + SOP task submission. Used with `useActionState`,
 * hence the `(prevState, formData)` signature.
 *
 * Flow:
 * 1. Create a proof upload record (initiates storage)
 * 2. Upload the file to the proof storage
 * 3. Complete the proof upload (finalizes metadata)
 * 4. Submit the SOP task (completes the field-worker obligation)
 *
 * Returns a discriminated ActionResult so the form can render the exact failure.
 */
export async function submitVaccinationProof(_prev: ActionResult | null, formData: FormData): Promise<ActionResult> {
  try {
    const obligationId = formData.get("obligationId");
    const sopTaskId = formData.get("sopTaskId");
    const file = formData.get("file") as File | null;

    if (!obligationId || typeof obligationId !== "string") {
      return { ok: false, error: "Missing obligation ID — proof cannot be created without a linked obligation" };
    }
    if (!sopTaskId || typeof sopTaskId !== "string") {
      return { ok: false, error: "Missing SOP task ID — proof submission cannot proceed without a task reference" };
    }
    if (!file || !(file instanceof File)) {
      return { ok: false, error: "No file selected — please choose a video or image file" };
    }

    const mediaType = inferMediaType(file.type);

    const createResult = await createProofUpload(obligationId);
    if (!createResult.ok) {
      return { ok: false, error: `Failed to create proof upload: ${createResult.error.message}` };
    }
    const proofId = createResult.data.proof.proof_id;

    const uploadResult = await uploadProofLocal(proofId, file);
    if (!uploadResult.ok) {
      return { ok: false, error: `Failed to upload file: ${uploadResult.error.message}` };
    }

    const completeResult = await completeProofUpload(proofId, mediaType);
    if (!completeResult.ok) {
      return { ok: false, error: `Failed to complete proof upload: ${completeResult.error.message}` };
    }

    const submitResult = await submitAppTask(sopTaskId, {
      proof_id: proofId,
      media_type: mediaType,
    });
    if (!submitResult.ok) {
      return { ok: false, error: `Failed to submit task: ${submitResult.error.message}` };
    }

    revalidatePath("/vaccination/execution", "page");
    return { ok: true };
  } catch (error) {
    return actionError(error);
  }
}

/**
 * Accept a vaccination completion (mark proof as verified).
 * Returns an ActionResult so the caller can surface a visible error on failure.
 */
export async function acceptCompletionAction(completionId: string): Promise<ActionResult> {
  try {
    if (!completionId || typeof completionId !== "string") {
      return { ok: false, error: "Missing completion ID — cannot accept without a valid completion record" };
    }

    const result = await acceptVaccinationCompletion(completionId);
    if (!result.ok) {
      return { ok: false, error: `Failed to accept completion: ${result.error.message}` };
    }

    revalidatePath("/vaccination/execution", "page");
    return { ok: true };
  } catch (error) {
    return actionError(error);
  }
}

/**
 * Reject a vaccination completion (mark proof as requiring rework).
 * Returns an ActionResult so the caller can surface a visible error on failure.
 */
export async function rejectCompletionAction(completionId: string, reason: string): Promise<ActionResult> {
  try {
    if (!completionId || typeof completionId !== "string") {
      return { ok: false, error: "Missing completion ID — cannot reject without a valid completion record" };
    }
    if (!reason || typeof reason !== "string" || reason.trim().length === 0) {
      return { ok: false, error: "Rejection reason required — provide a reason for requiring rework" };
    }

    const result = await rejectVaccinationCompletion(completionId, reason.trim());
    if (!result.ok) {
      return { ok: false, error: `Failed to reject completion: ${result.error.message}` };
    }

    revalidatePath("/vaccination/execution", "page");
    return { ok: true };
  } catch (error) {
    return actionError(error);
  }
}

/**
 * Infer media type from file MIME type.
 * Supports video/* and image/* types.
 */
function inferMediaType(mimeType: string): string {
  if (mimeType.startsWith("video/")) {
    return mimeType;
  }
  if (mimeType.startsWith("image/")) {
    return mimeType;
  }
  // Default to octet-stream if unknown
  return "application/octet-stream";
}
