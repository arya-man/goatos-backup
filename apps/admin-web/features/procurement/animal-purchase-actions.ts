"use server";

import { decideAnimalPurchaseAnimal } from "@/lib/api/procurement-server";

/**
 * The outcome of one decision, returned to the form that posted it (no redirect: the reviewer
 * stays exactly where they were on the page and the card updates in place). `code` is a SUFFIX
 * of a page-copy key (`action.<code>`): the page resolves it through the backend contract, so no
 * visible sentence is composed here.
 */
export type AnimalPurchaseDecisionState = {
  status: "idle" | "success" | "error";
  code: string;
  /** The decision that landed, so the card can show the right chip before the refresh. */
  decision?: "accept" | "reject";
  /** Bumps on every outcome so the same code twice still re-announces. */
  ticket: number;
};

// decideAnimalPurchaseAction is the CEO/CXO act on one filmed animal: Accept, or Reject with a
// note. The idempotency key is derived, not random — the same (candidate, row_version, decision)
// is one logical act, and row_version makes the key self-expiring once the decision lands, so a
// double click cannot record the animal twice and a stale form cannot overwrite a newer answer.
export async function decideAnimalPurchaseAction(
  previous: AnimalPurchaseDecisionState,
  formData: FormData,
): Promise<AnimalPurchaseDecisionState> {
  const ticket = previous.ticket + 1;
  const candidateId = String(formData.get("candidate_id") ?? "").trim();
  const decision = String(formData.get("decision") ?? "").trim();
  const note = String(formData.get("note") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? "");

  if (!candidateId || (decision !== "accept" && decision !== "reject")) {
    return { status: "error", code: "error_form", ticket };
  }
  if (!Number.isInteger(rowVersion) || rowVersion < 1) {
    return { status: "error", code: "error_form", ticket };
  }
  // The Reject button stays disabled until a note is typed; a form posted around that (or an old
  // tab) is refused here before the round trip rather than as a backend validation error.
  if (decision === "reject" && !note) {
    return { status: "error", code: "error_form", ticket };
  }

  const idempotencyKey = `animal-purchase-${candidateId}-${rowVersion}-${decision}`;
  const result = await decideAnimalPurchaseAnimal(
    candidateId,
    {
      decision,
      ...(note ? { note } : {}),
      row_version: rowVersion,
    },
    idempotencyKey,
  );
  // Deliberately no cache revalidation here: it would re-render the list inside the action's own response and
  // pull the card out from under the sentence the reviewer is reading. The form soft-refreshes
  // once the sentence has been on screen.
  if (!result.ok) {
    // 409 already_decided / row_version_conflict both mean "someone answered first": reload copy.
    const conflict = result.error.status === 409 || result.error.code === "already_decided" || result.error.code === "row_version_conflict";
    return { status: "error", code: conflict ? "decide_conflict" : "decide_failed", ticket };
  }
  return { status: "success", code: decision === "accept" ? "decided_accepted" : "decided_rejected", decision, ticket };
}
