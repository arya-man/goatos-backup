import { markUploadCompleted, markUploadFailed, markUploadStarted, type LocalProofDraft } from "../../../../../packages/media-client/src/index.js";

export function startProofUpload(draft: LocalProofDraft) {
  return markUploadStarted(draft);
}

export function completeProofUpload(draft: LocalProofDraft, proofId: string) {
  return markUploadCompleted(draft, proofId);
}

export function failProofUpload(draft: LocalProofDraft, error: string) {
  return markUploadFailed(draft, error);
}
