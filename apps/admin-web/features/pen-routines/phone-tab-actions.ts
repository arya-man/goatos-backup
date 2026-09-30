"use server";

// Server Actions for the phone tabs on /routines (maintainer instruction 2026-10-01,
// docs/decisions/simple-task-phone-tabs.md): create a tab, save one, retire or restore one. Every
// write goes to the backend under pen_routines.configure; this file only shapes the form and
// reports the outcome.
//
// RETURN THE ROW, NEVER REVALIDATE (docs/decisions/admin-web-interaction-patterns.md): the saved
// tab comes back to the drawer that posted it (`useActionState` in phone-tabs-section), which
// applies it to the section's own list in place. There is deliberately no revalidatePath here --
// re-reading the route on top of a returned row is the page flicker the interaction guard bans.
//
// This file composes no visible sentence. `code` is a SUFFIX of a page-copy key the drawer resolves
// through the contract; `detail` is the backend's OWN refusal sentence (the tab validation copy is
// farm wording, e.g. "a tab name of at most 24 characters is required") carried verbatim.

import { randomUUID } from "node:crypto";

import { createPenRoutineTab, setPenRoutineTabStatus, updatePenRoutineTab, type PenRoutineTab, type PenRoutineTabDetailResponse } from "@/lib/api/pen-routines-server";
import type { ApiResult } from "@/lib/api/server";
import { decodePhoneTabStatus, decodePhoneTabWrite } from "./phone-tab-model";

export type PhoneTabActionState = {
  status: "idle" | "success" | "error";
  /** Suffix of `tabs.action.<code>` in the page copy. */
  code: string;
  /** The backend's own sentence for a refusal, verbatim; empty when it sent none. */
  detail: string;
  /** The saved tab, for the section to apply in place; null unless `status` is success. */
  tab: PenRoutineTab | null;
  /** Bumps on every outcome so the same code twice still re-announces. */
  ticket: number;
};

function outcome(previous: PhoneTabActionState, result: ApiResult<PenRoutineTabDetailResponse>): PhoneTabActionState {
  const ticket = previous.ticket + 1;
  if (!result.ok) {
    // A 4xx carries the backend's refusal (invalid_tab, stale_tab, tab_not_found) in its envelope;
    // a transport/5xx failure has no farm sentence, so the drawer falls back to the contract copy.
    const detail = result.error.status !== undefined && result.error.status < 500 ? result.error.message : "";
    return { status: "error", code: "failed_message", detail, tab: null, ticket };
  }
  return { status: "success", code: "success_message", detail: "", tab: result.data.tab, ticket };
}

function fieldsMissing(previous: PhoneTabActionState): PhoneTabActionState {
  return { status: "error", code: "error_form", detail: "", tab: null, ticket: previous.ticket + 1 };
}

export async function createPhoneTabAction(previous: PhoneTabActionState, formData: FormData): Promise<PhoneTabActionState> {
  let body;
  try {
    body = decodePhoneTabWrite(formData);
  } catch {
    return fieldsMissing(previous);
  }
  // A create carries no fence; a fresh key per submit means a retry of THIS submit cannot create two tabs.
  delete body.row_version;
  return outcome(previous, await createPenRoutineTab(body, randomUUID()));
}

export async function updatePhoneTabAction(previous: PhoneTabActionState, formData: FormData): Promise<PhoneTabActionState> {
  const tabId = formData.get("tab_id");
  if (typeof tabId !== "string" || !tabId) return fieldsMissing(previous);
  let body;
  try {
    body = decodePhoneTabWrite(formData);
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(previous, await updatePenRoutineTab(tabId, body, randomUUID()));
}

export async function setPhoneTabStatusAction(previous: PhoneTabActionState, formData: FormData): Promise<PhoneTabActionState> {
  const tabId = formData.get("tab_id");
  if (typeof tabId !== "string" || !tabId) return fieldsMissing(previous);
  let body;
  try {
    body = decodePhoneTabStatus(formData);
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(previous, await setPenRoutineTabStatus(tabId, body, randomUUID()));
}
