export type LocalProofDraft = {
  local_id: string;
  proof_type: "photo" | "video" | "attachment";
  subject_type: "batch" | "goat" | "shed" | "task" | "other";
  subject_id?: string | null;
  local_uri: string;
  upload_state: "pending" | "uploading" | "completed" | "failed";
  proof_id?: string;
  error?: string;
};

export function markUploadStarted(draft: LocalProofDraft): LocalProofDraft {
  const { error: _error, ...rest } = draft;
  return { ...rest, upload_state: "uploading" };
}

export function markUploadCompleted(draft: LocalProofDraft, proofId: string): LocalProofDraft {
  const { error: _error, ...rest } = draft;
  return { ...rest, upload_state: "completed", proof_id: proofId };
}

export function markUploadFailed(draft: LocalProofDraft, error: string): LocalProofDraft {
  return { ...draft, upload_state: "failed", error };
}
