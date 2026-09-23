"use server";

import {
  deleteHealthConfigDiagnosisRoute,
  saveHealthConfigDiagnosisRoute,
  saveHealthConfigDiagnosisType,
} from "@/lib/api/server";

// =================================================================================================
// Diagnosis TYPE and ROUTING writes — who is judged by which rulebook (migration 000395).
//
// RULE 1 — THESE RETURN A RESULT AND NEVER REVALIDATE THE ROUTE. A server action may hand the
// caller a result to apply in place, or it may redirect, never both: revalidating on top of a
// returned result re-renders the route underneath a client still holding the answer, which is the
// page flicker the interaction-patterns decision bans. The caller decides what to do next.
//
// RULE 2 — NOTHING HERE NORMALISES A KEY. A type key and a stage code are JOIN keys: a stray
// upper-case letter "helpfully" folded here would point a stage at a different rulebook than the
// author typed. The backend lower-cases them inside the same validation that refuses a bad shape,
// so there is one answer rather than two that can drift.
//
// RULE 3 — THE BACKEND'S SENTENCE IS THE ONE SHOWN. A retire refused because stages still route
// to the type, a route naming an inactive type, a key already taken: each comes back with the
// domain's own words, and this layer passes them through rather than composing a second
// explanation of a rule it does not own.
// =================================================================================================

export type HealthTypeActionResult = {
  ok: boolean;
  /** A copy key in the health-config page contract, resolved by the client through `copy()`. */
  messageKey: string;
  /** The backend's own sentence, shown verbatim when present. */
  detail?: string;
  code?: string;
};

function failure(error: { code?: string; message?: string }): HealthTypeActionResult {
  return {
    ok: false,
    messageKey: "action.error_backend",
    detail: error.message,
    code: error.code,
  };
}

export async function saveDiagnosisType(
  input: { typeKey: string; label: string; status?: string; sortOrder?: number },
  idempotencyKey: string,
): Promise<HealthTypeActionResult> {
  const result = await saveHealthConfigDiagnosisType(
    {
      type_key: input.typeKey,
      label: input.label,
      status: input.status,
      sort_order: input.sortOrder,
    },
    idempotencyKey,
  );
  if (!result.ok) return failure(result.error);
  return { ok: true, messageKey: "action.type_saved" };
}

export async function saveDiagnosisRoute(
  input: { ageBand: string; stageCode: string; typeKey: string; subStage?: string },
  idempotencyKey: string,
): Promise<HealthTypeActionResult> {
  const result = await saveHealthConfigDiagnosisRoute(
    {
      age_band: input.ageBand,
      stage_code: input.stageCode,
      type_key: input.typeKey,
      sub_stage: input.subStage,
    },
    idempotencyKey,
  );
  if (!result.ok) return failure(result.error);
  return { ok: true, messageKey: "action.route_saved" };
}

export async function deleteDiagnosisRoute(
  input: { ageBand: string; stageCode: string },
  idempotencyKey: string,
): Promise<HealthTypeActionResult> {
  const result = await deleteHealthConfigDiagnosisRoute(
    { age_band: input.ageBand, stage_code: input.stageCode },
    idempotencyKey,
  );
  if (!result.ok) return failure(result.error);
  return { ok: true, messageKey: "action.route_removed" };
}
