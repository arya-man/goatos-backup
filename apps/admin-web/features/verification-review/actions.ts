"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  assignSopTask,
  correctWeighingObservationWeight,
  getSopTask,
  recordFeedWastageMeasurement,
  recordVerificationVerdict,
  requestSopTaskRework,
  type VerificationDecision,
} from "@/lib/api/server";

const PATHNAME = "/verify";

// A rework/re-assign on the source SOP task ripples across every screen that reads the
// process-integrity model (same fan-out as features/process-integrity/actions.ts).
function revalidateVaccinationViews(): void {
  for (const p of [PATHNAME, "/action-center", "/vaccination", "/protocol-adherence", "/workflows", "/"]) {
    revalidatePath(p);
  }
}

function redirectTarget(formData: FormData): URL {
  const returnTo = String(formData.get("return_to") ?? PATHNAME);
  const safe = returnTo.startsWith(PATHNAME) ? returnTo : PATHNAME;
  return new URL(safe, "https://admin.mesha.local");
}

function withFeedback(url: URL, status: "success" | "error", code: string): string {
  url.searchParams.set("va_status", status);
  url.searchParams.set("va_code", code);
  const qs = url.searchParams.toString();
  return qs ? `${url.pathname}?${qs}` : url.pathname;
}

// recordVerificationVerdictAction is the VERIFIER's act: approve or reject the proof video itself.
// It is the counterpart to the two authority actions below, which touch the source SOP task instead.
//
// row_version comes from the rendered item because it guards THAT row (unlike the rework/reassign
// actions below, whose row_version belongs to a different row and so must be re-fetched). A stale
// value is the correct failure here: it means someone recorded a verdict since this page rendered,
// and the backend's 409 is what stops this submit from silently overwriting it.
export async function recordVerificationVerdictAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const itemId = String(formData.get("item_id") ?? "").trim();
  const decision = String(formData.get("decision") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? "");

  if (!itemId || (decision !== "approved" && decision !== "rejected")) {
    redirect(withFeedback(url, "error", !itemId ? "missing_item_handle" : "invalid_decision"));
  }
  if (!Number.isInteger(rowVersion) || rowVersion < 1) {
    redirect(withFeedback(url, "error", "missing_row_version"));
  }
  // The backend owns this rule (422 on a reasonless rejection); checking here too keeps the
  // operator from losing a page round-trip to learn it.
  if (decision === "rejected" && !reason) {
    redirect(withFeedback(url, "error", "missing_reason"));
  }

  // Idempotency identity for this verdict. Derived, not random, so a double-click or a retried
  // Server Action is ONE write: the same (item, row_version, decision) is the same logical act.
  // row_version makes it self-expiring — once the verdict lands the row moves on, so a later,
  // legitimately different verdict can never collide with this key.
  const idempotencyKey = `verification-verdict-${itemId}-${rowVersion}-${decision}`;
  const result = await recordVerificationVerdict(
    itemId,
    {
      decision: decision as VerificationDecision,
      // Send reason only when rejecting: the schema marks it required-when-rejected, and an empty
      // string on an approval is a value the contract does not ask for.
      ...(decision === "rejected" ? { reason } : {}),
      row_version: rowVersion,
    },
    idempotencyKey,
  );
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  // An APPROVAL advances to the next video instead of dropping back to the list (maintainer ask,
  // 2026-08-19): on the pending tab the approved item leaves the filtered list, so returning with
  // its own vi_row no longer resolved and the drawer closed. The drawer sends the id of the row
  // that FOLLOWED this one at render time. When this page is exhausted but keyset pagination has
  // another page, follow that cursor; only without both a next row and a next cursor is the queue
  // done. A rejection keeps its current return unchanged.
  if (decision === "approved") {
    const nextRow = String(formData.get("next_row") ?? "").trim();
    const nextCursor = String(formData.get("next_cursor") ?? "").trim();
    const nextTrail = String(formData.get("next_trail") ?? "").trim();
    url.searchParams.delete("vi_open_first");
    if (nextRow) url.searchParams.set("vi_row", nextRow);
    else {
      url.searchParams.delete("vi_row");
      if (nextCursor) {
        url.searchParams.set("vi_cursor", nextCursor);
        url.searchParams.set("vi_open_first", "1");
      }
      if (nextTrail) url.searchParams.set("vi_trail", nextTrail);
    }
  }
  redirect(withFeedback(url, "success", decision === "approved" ? "verdict_approved" : "verdict_rejected"));
}

// correctWeightAction is the VERIFIER's weight correction (maintainer decision 2026-08-17): she
// watches the proof video and replaces the number the operator typed. It is a separate act from her
// verdict on purpose -- she may correct before deciding or after, including on an item she already
// approved, until the bucket closes -- so it is its own form and its own action.
//
// The observation id and ref type come from the item's backend-owned measurement_correction block,
// which echoes source.ref_id/source.ref_type. This action never composes that address itself.
export async function correctWeightAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const observationId = String(formData.get("observation_id") ?? "").trim();
  const refType = String(formData.get("ref_type") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  const rawWeight = String(formData.get("weight_kg") ?? "").trim();
  const rawCount = String(formData.get("animal_count") ?? "").trim();

  if (!observationId || !refType) {
    redirect(withFeedback(url, "error", "missing_item_handle"));
  }
  // A blank field is "she has not typed a weight", NOT a zero. Coercing blank to 0 here would send a
  // value she never entered and come back as "out of range", blaming her for the client's bug.
  if (!rawWeight) {
    redirect(withFeedback(url, "error", "missing_weight"));
  }
  const weightKg = Number(rawWeight);
  if (!Number.isFinite(weightKg)) {
    redirect(withFeedback(url, "error", "weight_out_of_range"));
  }

  // The head count is LUMP-SUM ONLY and optional: blank means "leave the recorded count alone".
  // It is omitted entirely rather than sent as 0 on an individual capture, where the backend
  // refuses a head count outright.
  let animalCount: number | undefined;
  if (rawCount) {
    const parsed = Number(rawCount);
    if (!Number.isInteger(parsed) || parsed < 0) {
      redirect(withFeedback(url, "error", "animal_count_out_of_range"));
    }
    animalCount = parsed;
  }

  // Derived, not random, so a double-click or a retried Server Action is ONE write. The weight is
  // part of the key: correcting to 12 kg and then to 13 kg are two different acts and must not
  // collide, while re-sending the SAME correction replays for free.
  const idempotencyKey = `weighing-weight-correction-${observationId}-${weightKg}-${animalCount ?? "keep"}`;
  const result = await correctWeighingObservationWeight(
    observationId,
    {
      ref_type: refType as "weighing_observation" | "weighing_shed_observation",
      weight_kg: weightKg,
      ...(animalCount === undefined ? {} : { animal_count: animalCount }),
      ...(reason ? { reason } : {}),
    },
    idempotencyKey,
  );
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "weight_corrected"));
}

// recordWastageMeasurementAction is the VERIFIER's feed-wastage measurement (maintainer decision
// 2026-08-18): she watches the pen's leftover-feed video and records the weight she can read in
// it. Like the weight correction above it is a separate act from her verdict — she records the
// value and then approves; a clip whose value she cannot read is rejected instead, and nothing is
// recorded.
//
// The completion id comes from the item's backend-owned measurement_correction block, which echoes
// source.ref_id. This action never composes that address itself.
export async function recordWastageMeasurementAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const completionId = String(formData.get("completion_id") ?? "").trim();
  const rawWastage = String(formData.get("wastage_kg") ?? "").trim();

  if (!completionId) {
    redirect(withFeedback(url, "error", "missing_item_handle"));
  }
  // A blank field is "she has not typed a value", NOT a zero. Zero is a VALID measurement here (an
  // empty trough), which is exactly why blank must never be coerced into it: sending 0 for a blank
  // would record a measurement she never made.
  if (!rawWastage) {
    redirect(withFeedback(url, "error", "missing_wastage"));
  }
  const wastageKg = Number(rawWastage);
  if (!Number.isFinite(wastageKg)) {
    redirect(withFeedback(url, "error", "wastage_out_of_range"));
  }

  // Derived, not random, so a double-click or a retried Server Action is ONE write. The value is
  // part of the key: recording 3 kg and then 3.5 kg are two different acts and must not collide,
  // while re-sending the SAME value replays for free.
  const idempotencyKey = `feed-wastage-measurement-${completionId}-${wastageKg}`;
  const result = await recordFeedWastageMeasurement(
    completionId,
    { wastage_kg: wastageKg },
    idempotencyKey,
  );
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "wastage_recorded"));
}

// reworkVerificationItemAction requests SOP rework on the verification item's SOURCE task. The
// verifier's rejected verdict is advisory; this is the authority's real act. Fetches the task's
// CURRENT row_version first (the verification_item's row_version guards a different row) so the
// optimistic-concurrency write does not race a stale value carried in the form.
export async function reworkVerificationItemAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const taskId = String(formData.get("task_id") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!taskId || !reason) {
    redirect(withFeedback(url, "error", !taskId ? "missing_task_handle" : "missing_reason"));
  }

  const task = await getSopTask(taskId);
  if (!task.ok) {
    redirect(withFeedback(url, "error", task.error.code ?? task.error.kind));
  }

  const result = await requestSopTaskRework(taskId, { reason, row_version: task.data.task.row_version });
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "rework_requested"));
}

// reassignVerificationItemAction assigns the verification item's SOURCE task to a different staff
// position seat, the authority's "re-assign owner" act.
export async function reassignVerificationItemAction(formData: FormData): Promise<void> {
  const url = redirectTarget(formData);
  const taskId = String(formData.get("task_id") ?? "").trim();
  const assignedTo = String(formData.get("assigned_to") ?? "").trim();
  const reason = String(formData.get("reason") ?? "").trim();
  if (!taskId || !assignedTo || !reason) {
    redirect(
      withFeedback(url, "error", !taskId ? "missing_task_handle" : !assignedTo ? "missing_assignee" : "missing_reason"),
    );
  }

  const task = await getSopTask(taskId);
  if (!task.ok) {
    redirect(withFeedback(url, "error", task.error.code ?? task.error.kind));
  }

  const result = await assignSopTask(taskId, { assigned_to: assignedTo, reason, row_version: task.data.task.row_version });
  revalidateVaccinationViews();
  if (!result.ok) {
    redirect(withFeedback(url, "error", result.error.code ?? result.error.kind));
  }
  redirect(withFeedback(url, "success", "reassigned"));
}

// loadReassignPositionsAction was REMOVED with the re-assign picker (maintainer decision
// 2026-08-07). The verifier's screen carries only her verdict now, so nothing on it reads the
// staff roster -- which also retires the 500-row SSR fetch this page used to make on every load.
//
// reworkVerificationItemAction / reassignVerificationItemAction above are deliberately KEPT: they
// are real, wired writes (requestSopTaskRework / assignSopTask) belonging to the authority surface
// that owns source-task action. Only their placement on the verifier's review screen was wrong.
