"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  assignSopTask,
  getSopTask,
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

// readMeasurement pulls the verifier's reading out of the verdict form.
//
// A BLANK FIELD IS NOT A ZERO. Blank means she entered nothing -- the normal weighing case, where
// the operator's recorded weight stands -- while 0 is a real reading for wastage (an empty trough).
// Coercing one into the other would either record a weight she never typed or silently discard a
// measurement she did.
type MeasurementRead =
  | { ok: true; measurement?: { value: number; count?: number; reason?: string } }
  | { ok: false; code: "invalid_measurement" | "invalid_measurement_count" };

function readMeasurement(formData: FormData): MeasurementRead {
  const raw = String(formData.get("measurement_value") ?? "").trim();
  const countRaw = String(formData.get("measurement_count") ?? "").trim();
  if (raw === "" && countRaw === "") return { ok: true };
  const value = Number(raw);
  if (raw === "" || !Number.isFinite(value) || value < 0) return { ok: false, code: "invalid_measurement" };
  const count = countRaw === "" ? undefined : Number(countRaw);
  if (count !== undefined && (!Number.isInteger(count) || count < 1)) {
    return { ok: false, code: "invalid_measurement_count" };
  }
  const note = String(formData.get("measurement_reason") ?? "").trim();
  return {
    ok: true,
    measurement: {
      value,
      ...(count !== undefined ? { count } : {}),
      ...(note ? { reason: note } : {}),
    },
  };
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
  // THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20). The measurement field lives
  // INSIDE the verdict form now, so the verifier types the value she reads off the video and
  // presses Accept once. It replaces the separate save form, whose save relabelled the item,
  // bumped row_version, and left the Accept she pressed next fenced out with a 409.
  const measurementRead = readMeasurement(formData);

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
  if (!measurementRead.ok) {
    redirect(withFeedback(url, "error", measurementRead.code));
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
      // Only on an approve. A rejection sends the work back to be recorded again, so a value typed
      // before she changed her mind must not land on a record about to be redone. The backend drops
      // it too; sending it would just be a value the contract does not ask for.
      ...(decision === "approved" && measurementRead.measurement ? { measurement: measurementRead.measurement } : {}),
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

// The two standalone measurement actions that used to live here -- correctWeightAction (weighing,
// 2026-08-17) and recordWastageMeasurementAction (feed wastage, 2026-08-18) -- are DELETED.
//
// THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20). Their save-then-approve pair was
// two acts for one judgement, and the save relabelled the verification item, which bumps
// row_version, so the Accept the verifier pressed next was fenced out with a 409 and silently did
// nothing. The value now travels on recordVerificationVerdictAction above, and the backend applies
// it and the verdict together.
//
// Deleted rather than left unwired: a server action nothing calls reads to the next author as a
// path still in use. The producing modules' own routes stay served -- an installed APK built before
// this decision still posts to them -- but nothing in admin-web does.

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
