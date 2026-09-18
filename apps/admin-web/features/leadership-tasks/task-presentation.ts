import type { Tone } from "@/components/ui-primitives";

import type { TaskRow } from "./task-row";

/**
 * The two pure mappings the Tasks desk's SERVER components need.
 *
 * They lived in `leadership-tasks-table.tsx`, which is a `"use client"` module — and a plain
 * function exported from a client module is a CLIENT REFERENCE, not a function a server component
 * may call. The status board and the detail panel are server-rendered, so calling them there threw
 * at render ("Attempted to call initials() from the server but initials is on the client") and the
 * avatar-bearing subtrees came back as error boundaries: a page that typechecked, linted and
 * passed the unit suite, and was blank where the people were.
 *
 * This module carries NO directive, so it is usable from both sides. Nothing here is a decision:
 * the status VOCABULARY and its chip wording are the backend's, and only the tone token is chosen
 * here so one status reads the same on the table's chip, the board's column accent and the
 * panel's status control.
 */
export function statusTone(status: TaskRow["status"]): Tone {
  if (status === "in_progress") return "info";
  if (status === "done") return "ok";
  if (status === "cancelled") return "mut";
  return "warn";
}

/** The avatar's letters. The NAME is always rendered beside it; this is never the only label. */
export function initials(name: string): string {
  return name
    .split(/\s+/)
    .map((part) => part.charAt(0))
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

/**
 * The plain-English sentence for a status-change refusal, per backend error code.
 *
 * `TaskFeedbackBanner` looks every outcome up as `feedback.<code>` in the page contract, which is
 * right — the code space is the backend's own error vocabulary and the backend owns the copy. But
 * with an EMPTY fallback, a contract that has not authored these keys yet collapses three
 * genuinely different refusals into one "something went wrong": the reader cannot tell "someone
 * else changed this task" from "this task is closed" from "you are not the person who may do
 * this", and on the board those three are the three ways a drag can bounce. So the banner passes
 * this as the THIRD argument to `copy` — the contract still wins the moment its owner authors the
 * key, and until then each code reads as itself.
 *
 * `version_conflict` is worded as a statement of what already happened, not a promise: the action
 * `revalidatePath`s and redirects, so by the time this sentence is on screen the board has been
 * re-read and is showing the other actor's result.
 */
export function statusChangeFeedbackFallback(code: string): string {
  switch (code) {
    case "version_conflict":
      return "Someone else changed this task first, so your change was not applied. The board has been refreshed with their version — try again from what you see now.";
    case "task_closed":
      return "This task is closed, so its status cannot be changed any more.";
    case "not_assignee":
      return "Only the person this task is assigned to can move it.";
    case "not_raiser":
      return "Only the person who raised this task can cancel it.";
    case "invalid_status_transition":
      return "That is not a move this task can make from where it is now.";
    case "task_not_found":
      return "This task is no longer on the list.";
    default:
      return "";
  }
}
