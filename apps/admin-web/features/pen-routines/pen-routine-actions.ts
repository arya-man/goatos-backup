"use server";

// Server Actions for /routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md):
// create a routine, save a new version of one, and pause / resume / retire one. Every write goes
// to the backend under its own permission (pen_routines.configure); this file only shapes the
// form and reports the outcome.
//
// EVERY ACTION LANDS IN PLACE (the market-config rule, maintainer report 2026-09-15): the outcome
// is RETURNED to the form that posted it (`useActionState` in routine-drawer), and the
// `revalidatePath` re-reads the page's server data inside the same response, so the new or
// changed routine appears in the table under the reader's eyes. Nothing navigates.
//
// This file composes no visible sentence of its own. `code` is a SUFFIX of a page-copy key the
// drawer resolves through the backend contract; `detail` is the backend's OWN refusal sentence
// (the domain's validation copy is farm wording) carried through verbatim when it sent one.

import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";

import { createPenRoutine, setPenRoutineStatus, updatePenRoutine } from "@/lib/api/pen-routines-server";
import type { ApiResult } from "@/lib/api/server";
import { decodePenRoutineWrite } from "./routine-form-model";

const ROUTINES_PATH = "/routines";

export type PenRoutineActionState = {
  status: "idle" | "success" | "error";
  /** Suffix of `action.<code>` in the page copy: `success_message` or `failed_message`. */
  code: string;
  /** The backend's own sentence for a refusal, verbatim; empty when it sent none. */
  detail: string;
  /** Bumps on every outcome so the same code twice still re-announces. */
  ticket: number;
};

function outcome(previous: PenRoutineActionState, result: ApiResult<unknown>): PenRoutineActionState {
  const ticket = previous.ticket + 1;
  if (!result.ok) {
    // A 4xx carries the backend's refusal in its envelope; a transport/5xx failure has no farm
    // sentence, so the drawer falls back to the contract's generic failure copy.
    const detail = result.error.status !== undefined && result.error.status < 500 ? result.error.message : "";
    return { status: "error", code: "failed_message", detail, ticket };
  }
  revalidatePath(ROUTINES_PATH);
  return { status: "success", code: "success_message", detail: "", ticket };
}

function fieldsMissing(previous: PenRoutineActionState): PenRoutineActionState {
  return { status: "error", code: "error_form", detail: "", ticket: previous.ticket + 1 };
}

export async function createRoutineAction(previous: PenRoutineActionState, formData: FormData): Promise<PenRoutineActionState> {
  let body;
  try {
    body = decodePenRoutineWrite(formData);
  } catch {
    return fieldsMissing(previous);
  }
  // A fresh key per submit: a retry of THIS submit cannot create the routine twice.
  return outcome(previous, await createPenRoutine(body, randomUUID()));
}

export async function updateRoutineAction(previous: PenRoutineActionState, formData: FormData): Promise<PenRoutineActionState> {
  const routineId = formData.get("routine_id");
  if (typeof routineId !== "string" || !routineId) return fieldsMissing(previous);
  let body;
  try {
    body = decodePenRoutineWrite(formData);
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(previous, await updatePenRoutine(routineId, body, randomUUID()));
}

export async function setRoutineStatusAction(previous: PenRoutineActionState, formData: FormData): Promise<PenRoutineActionState> {
  const routineId = formData.get("routine_id");
  const status = formData.get("status");
  if (typeof routineId !== "string" || !routineId) return fieldsMissing(previous);
  if (status !== "active" && status !== "paused" && status !== "retired") return fieldsMissing(previous);
  const rowVersionRaw = formData.get("row_version");
  const rowVersion = typeof rowVersionRaw === "string" ? Number.parseInt(rowVersionRaw, 10) : Number.NaN;
  return outcome(
    previous,
    await setPenRoutineStatus(routineId, { status, row_version: Number.isFinite(rowVersion) ? rowVersion : 0 }, randomUUID()),
  );
}
