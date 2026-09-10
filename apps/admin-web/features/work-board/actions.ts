"use server";

// The drawer's Flag button: raises a Leadership Task to the park head from a board row. The
// backend composes the brief and resolves the park head; this action only forwards the row's
// backend-owned strings and the director's note, then redirects with backend-driven feedback.
import { randomUUID } from "node:crypto";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import { raiseWorkBoardFlag } from "@/lib/api/work-board-server";

// Every actionKey MUST start with "action." and have matching page-contract copy.
function feedbackKeyFor(code: string | undefined): string {
  switch (code) {
    case "park_head_missing":
      return "action.flag_park_head_missing";
    case "flag_to_self":
      return "action.flag_to_self";
    case "park_head_not_reachable":
      return "action.flag_park_head_not_reachable";
    default:
      return "action.flag_failed";
  }
}

export async function flagParkHeadAction(formData: FormData): Promise<void> {
  const result = await raiseWorkBoardFlag(
    {
      row_key: requiredString(formData, "row_key"),
      park_id: requiredString(formData, "park_id"),
      row_title: requiredString(formData, "row_title"),
      row_subtitle: optionalString(formData, "row_subtitle"),
      pen_display: optionalString(formData, "pen_display"),
      clock_label: optionalString(formData, "clock_label"),
      note: optionalString(formData, "note"),
    },
    randomUUID(),
  );
  if (!result.ok) actionRedirect(formData, "error", feedbackKeyFor(result.error.code));
  actionRedirect(formData, "success", "action.flag_raised");
}
