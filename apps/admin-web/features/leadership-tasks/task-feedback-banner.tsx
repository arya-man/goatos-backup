"use client";

import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import { useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { refusalSentence } from "./task-feedback-copy";

/**
 * What every write on this page says when it lands.
 *
 * `actions.ts` has always stamped the outcome on the redirect URL and NOTHING read it: a status
 * change that came back 409 `version_conflict`, an attachment that failed to upload and a task
 * that was raised successfully all looked identical — the page simply re-rendered. This is the
 * reader.
 *
 * The code space is OPEN: it is whatever the backend's error vocabulary contains, so each code is
 * looked up as `feedback.<code>` with an explicit absent-value fallback (the one case `copy`
 * allows one) and an unrecognised code falls back to the contract's generic success/failure
 * sentence rather than rendering the raw token.
 *
 * ── WHY A REFUSAL NAMES THINGS, AND WHAT IT IS ALLOWED TO NAME ────────────────────────────────
 * "Action could not be completed" is exactly the abstract wording AGENTS.md bans: a reader on a
 * board of 424 tasks cannot act on it, and the three refusals a status change can return are
 * three different situations. So a refusal is rendered from a SENTENCE TEMPLATE in the page
 * contract with two backend-supplied values substituted in:
 *
 *   {status}  the task's current `status_chip`, re-read after the refusal — the backend's own chip
 *             wording ("In progress", "Done", "Cancelled"), verbatim. On a `version_conflict` this is
 *             the point: the board the reader is looking at is out of date, and this is the truth.
 *   {name}    the person whose move it is, by name, off the task itself — the assignee for a
 *             ladder move, the raiser for a cancel.
 *
 * NOTHING IS INVENTED AND NOTHING IS A PLACEHOLDER. A value the backend did not supply makes this
 * component fall to the template that does not mention it (`…_status` without the name, then the
 * plain key), which is the repo's rule for an unresolvable identifier: drop the clause rather
 * than render "someone" or a raw id. The templates live in the contract like every other visible
 * string; the fallbacks here are what the page says until its owner authors the keys.
 *
 * It does NOT claim WHO made the change on a conflict. Neither the 409 envelope nor
 * `GET /app/leadership-tasks/{task_id}` reports the actor of the last status change, and deducing
 * one from the transition would be inventing a name. See `actions.ts` for the backend follow-up.
 */
export function TaskFeedbackBanner({
  pageContract,
  status,
  code,
  statusNow,
  who,
}: {
  pageContract: AdminUiPageContract;
  status: string;
  code?: string;
  /** The task's current backend `status_chip`, when the action re-read it. */
  statusNow?: string;
  /** The name of the person whose move it is, when the task named one. */
  who?: string;
}) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;
  const success = status === "success";
  // The contract lookup is handed in as a function so the wording module needs no imports at all
  // (see `task-feedback-copy.ts`); every sentence is still `copy(contract, key, fallback)`.
  const specific = code
    ? refusalSentence(
        (key, fallback) => copy(pageContract, key, fallback),
        code,
        statusNow,
        who,
      )
    : "";
  const message =
    specific ||
    copy(pageContract, success ? "action.success_message" : "action.failed_message");
  // Template MUI Alert (Minimal alert-view: standard severity + AlertTitle + onClose): the
  // severity carries the ok / danger tone, `closeText` is the dismiss button's accessible name.
  return (
    <Alert
      severity={success ? "success" : "error"}
      role={success ? "status" : "alert"}
      aria-live="polite"
      onClose={() => setDismissed(true)}
      closeText={copy(pageContract, "action.close")}
      sx={{ mb: 2 }}
    >
      <AlertTitle>
        {success
          ? copy(pageContract, "action.success_title", "Done")
          : copy(pageContract, "action.failed_title")}
      </AlertTitle>
      {message}
    </Alert>
  );
}
