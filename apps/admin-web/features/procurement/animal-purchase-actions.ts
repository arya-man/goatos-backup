"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { decideAnimalPurchaseAnimal } from "@/lib/api/procurement-server";

const PATHNAME = "/procurement/animal-purchases";

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

// Redirect-feedback shape shared with the toxin verdict (tx_status/tx_code) under page-scoped
// params. `ap_code` is a SUFFIX of a page-copy key (`action.<code>`): the page resolves it through
// the backend contract, so no visible sentence is composed here.
function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("ap_status", status);
  url.searchParams.set("ap_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

// decideAnimalPurchaseAction is the CEO/CXO act on one filmed animal: Accept, or Reject with a
// note. The idempotency key is derived, not random — the same (candidate, row_version, decision)
// is one logical act, and row_version makes the key self-expiring once the decision lands, so a
// double click cannot record the animal twice and a stale form cannot overwrite a newer answer.
export async function decideAnimalPurchaseAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const candidateId = String(formData.get("candidate_id") ?? "").trim();
  const decision = String(formData.get("decision") ?? "").trim();
  const note = String(formData.get("note") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? "");

  if (!candidateId || (decision !== "accept" && decision !== "reject")) {
    redirect(withFeedback(url, "error", "error_form"));
  }
  if (!Number.isInteger(rowVersion) || rowVersion < 1) {
    redirect(withFeedback(url, "error", "error_form"));
  }
  // The Reject button stays disabled until a note is typed; a form posted around that (or an old
  // tab) is refused here before the round trip rather than as a backend validation error.
  if (decision === "reject" && !note) {
    redirect(withFeedback(url, "error", "error_form"));
  }

  const idempotencyKey = `animal-purchase-${candidateId}-${rowVersion}-${decision}`;
  const result = await decideAnimalPurchaseAnimal(
    candidateId,
    {
      decision: decision as "accept" | "reject",
      ...(note ? { note } : {}),
      row_version: rowVersion,
    },
    idempotencyKey,
  );
  revalidatePath(PATHNAME);
  if (!result.ok) {
    // 409 already_decided / row_version_conflict both mean "someone answered first": reload copy.
    const conflict = result.error.status === 409 || result.error.code === "already_decided" || result.error.code === "row_version_conflict";
    redirect(withFeedback(url, "error", conflict ? "decide_conflict" : "decide_failed"));
  }
  redirect(withFeedback(url, "success", decision === "accept" ? "decided_accepted" : "decided_rejected"));
}
