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
 * Submit a vaccination proof upload + SOP task submission.
 *
 * Flow:
 * 1. Create a proof upload record (initiates storage)
 * 2. Upload the file to the proof storage
 * 3. Complete the proof upload (finalizes metadata)
 * 4. Submit the SOP task (completes the field-worker obligation)
 *
 * Server-side guards:
 * - Both obligationId and sopTaskId must be present (non-null)
 * - mediaType is inferred from file MIME type
 *
 * Returns: void (follows server action form convention)
 */
export async function submitVaccinationProof(formData: FormData): Promise<void> {
  try {
    const obligationId = formData.get("obligationId");
    const sopTaskId = formData.get("sopTaskId");
    const file = formData.get("file") as File | null;

    // Server-side validation
    if (!obligationId || typeof obligationId !== "string") {
      throw new Error("Missing obligation ID — proof cannot be created without a linked obligation");
    }
    if (!sopTaskId || typeof sopTaskId !== "string") {
      throw new Error("Missing SOP task ID — proof submission cannot proceed without a task reference");
    }
    if (!file || !(file instanceof File)) {
      throw new Error("No file selected — please choose a video or image file");
    }

    // Infer media type from MIME type
    const mediaType = inferMediaType(file.type);

    // Step 1: Create proof upload
    const createResult = await createProofUpload(obligationId);
    if (!createResult.ok) {
      throw new Error(`Failed to create proof upload: ${createResult.error.message}`);
    }
    const proofId = createResult.data.proof.proof_id;

    // Step 2: Upload file
    const uploadResult = await uploadProofLocal(proofId, file);
    if (!uploadResult.ok) {
      throw new Error(`Failed to upload file: ${uploadResult.error.message}`);
    }

    // Step 3: Complete proof upload
    const completeResult = await completeProofUpload(proofId, mediaType);
    if (!completeResult.ok) {
      throw new Error(`Failed to complete proof upload: ${completeResult.error.message}`);
    }

    // Step 4: Submit SOP task
    const submitResult = await submitAppTask(sopTaskId, {
      proof_id: proofId,
      media_type: mediaType,
    });
    if (!submitResult.ok) {
      throw new Error(`Failed to submit task: ${submitResult.error.message}`);
    }

    // Revalidate execution path to reflect proof status change
    revalidatePath("/vaccination/execution", "page");
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    // Log the error; client should handle via a toast/error display
    console.error("Vaccination proof submission failed:", message);
    throw error;
  }
}

/**
 * Accept a vaccination completion (mark proof as verified).
 *
 * Server-side guard: completionId must be non-null.
 * Returns: void (follows server action convention)
 */
export async function acceptCompletionAction(completionId: string): Promise<void> {
  try {
    if (!completionId || typeof completionId !== "string") {
      throw new Error("Missing completion ID — cannot accept without a valid completion record");
    }

    const result = await acceptVaccinationCompletion(completionId);
    if (!result.ok) {
      throw new Error(`Failed to accept completion: ${result.error.message}`);
    }

    revalidatePath("/vaccination/execution", "page");
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    console.error("Accept completion failed:", message);
    throw error;
  }
}

/**
 * Reject a vaccination completion (mark proof as requiring rework).
 *
 * Server-side guards:
 * - completionId must be non-null
 * - reason must be a non-empty string
 *
 * Returns: void (follows server action convention)
 */
export async function rejectCompletionAction(completionId: string, reason: string): Promise<void> {
  try {
    if (!completionId || typeof completionId !== "string") {
      throw new Error("Missing completion ID — cannot reject without a valid completion record");
    }
    if (!reason || typeof reason !== "string" || reason.trim().length === 0) {
      throw new Error("Rejection reason required — provide a reason for requiring rework");
    }

    const result = await rejectVaccinationCompletion(completionId, reason.trim());
    if (!result.ok) {
      throw new Error(`Failed to reject completion: ${result.error.message}`);
    }

    revalidatePath("/vaccination/execution", "page");
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unknown error";
    console.error("Reject completion failed:", message);
    throw error;
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
