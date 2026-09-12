"use server";

// The drawer's Flag button: raises a Leadership Task to the park head from a board row. The
// backend looks the row up on the caller's own board, composes the brief and resolves the park
// head; this action only names the row (key, park, day) and forwards the director's note, then
// redirects with backend-driven feedback.
import { randomUUID } from "node:crypto";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import { listWorkBoardSubtasks, raiseWorkBoardFlag, type WorkBoardSubtaskPage } from "@/lib/api/work-board-server";

// Every actionKey MUST start with "action." and have matching page-contract copy.
function feedbackKeyFor(code: string | undefined): string {
  switch (code) {
    case "park_head_missing":
      return "action.flag_park_head_missing";
    case "flag_to_self":
      return "action.flag_to_self";
    case "park_head_not_reachable":
      return "action.flag_park_head_not_reachable";
    case "row_not_found":
      return "action.flag_row_not_found";
    default:
      return "action.flag_failed";
  }
}

export async function flagParkHeadAction(formData: FormData): Promise<void> {
  const result = await raiseWorkBoardFlag(
    {
      row_key: requiredString(formData, "row_key"),
      park_id: requiredString(formData, "park_id"),
      business_date: requiredString(formData, "business_date"),
      note: optionalString(formData, "note"),
    },
    randomUUID(),
  );
  if (!result.ok) actionRedirect(formData, "error", feedbackKeyFor(result.error.code));
  actionRedirect(formData, "success", "action.flag_raised");
}

export type SubtasksActionResult = { ok: true; page: WorkBoardSubtaskPage } | { ok: false; code?: string };

// The issue view's subtask page: fetched only when a card is open, for that card, through an
// authenticated Server Action (never a client fetch, never on the list read).
export async function loadSubtasksAction(input: { rowKey: string; park: string; businessDate: string; owner?: string; cursor?: string }): Promise<SubtasksActionResult> {
  const result = await listWorkBoardSubtasks(input.rowKey, { park: input.park, businessDate: input.businessDate, owner: input.owner }, { limit: 10, cursor: input.cursor });
  if (!result.ok) return { ok: false, code: result.error.code };
  return { ok: true, page: result.data };
}
