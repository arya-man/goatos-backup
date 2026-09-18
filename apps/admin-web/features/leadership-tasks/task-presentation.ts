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

