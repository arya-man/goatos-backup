"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import {
  assignSopTask,
  getProofDownloadUrl,
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

function verdictRequestFingerprint(value: unknown): string {
  const text = JSON.stringify(value);
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(36);
}

// The backend's one-time refusal of an out-of-tolerance packing reading (see recordVerificationVerdictAction).
const VARIANCE_CONFIRM_CODE = "measurement_confirmation_required";
// Field errors on that refusal address each flagged entry box by its key.
const VARIANCE_FIELD_PREFIX = "measurement.entries.";
const FEED_WASTAGE_REF_TYPE = "feed_wastage_completion";

// Explicit media open/play bridge for admin-web. Drawer/list hydration must carry only proof IDs
// or backend proof routes; this server action resolves one browser-usable signed URL only after a
// reviewer clicks the proof.
export async function resolveVerificationProofMediaUrl(proofRef: string): Promise<string | null> {
  const trimmed = proofRef.trim();
  if (!trimmed) return null;
  return getProofDownloadUrl(trimmed);
}

// readMeasurement pulls the verifier's reading out of the verdict form.
//
// A BLANK FIELD IS NOT A ZERO. Blank means she entered nothing, while 0 is a real reading for
// wastage (an empty trough). Weighing is different again: the verifier's reading is required and
// must be positive, so 0 is refused here before the request leaves admin-web.
type MeasurementRead =
  | {
      ok: true;
      measurement?: {
        value?: number;
        count?: number;
        reason?: string;
        entries?: { key: string; value: number }[];
        variance_acknowledged?: boolean;
      };
    }
  | { ok: false; code: "invalid_measurement" | "invalid_measurement_count" };

function readMeasurement(formData: FormData): MeasurementRead {
  // Per-field readings (feed packing's blind entry, maintainer decision 2026-08-21): one input per
  // feed item, named measurement_entry:<key>. A blank box means "not entered" and is DROPPED here
  // -- the backend's completeness check then names the missing field instead of recording a guess.
  // Zero is a real reading ("this item was not packed") and goes through.
  const entries: { key: string; value: number }[] = [];
  for (const [name, raw] of formData.entries()) {
    if (!name.startsWith("measurement_entry:")) continue;
    const key = name.slice("measurement_entry:".length).trim();
    const trimmed = String(raw ?? "").trim();
    if (!key || trimmed === "") continue;
    const value = Number(trimmed);
    if (!Number.isFinite(value) || value < 0) return { ok: false, code: "invalid_measurement" };
    entries.push({ key, value });
  }

  const raw = String(formData.get("measurement_value") ?? "").trim();
  const refType = String(formData.get("measurement_ref_type") ?? "").trim();
  const countRaw = String(formData.get("measurement_count") ?? "").trim();
  if (raw === "" && countRaw === "" && entries.length === 0) return { ok: true };
  if (entries.length > 0) {
    // Her "I checked the video again" after a 422 measurement_confirmation_required (maintainer
    // decision 2026-09-09). Sent only when ticked, and only with per-field readings -- the flag
    // means nothing on its own.
    const acknowledged = String(formData.get("variance_acknowledged") ?? "") === "1";
    return { ok: true, measurement: { entries, ...(acknowledged ? { variance_acknowledged: true } : {}) } };
  }
  const value = Number(raw);
  if (raw === "" || !Number.isFinite(value) || value < 0) return { ok: false, code: "invalid_measurement" };
  if (refType !== FEED_WASTAGE_REF_TYPE && value <= 0) return { ok: false, code: "invalid_measurement" };
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

  const request = {
    decision: decision as VerificationDecision,
    // Sent whenever she typed one: required on a reject (checked above and by the backend's
    // 422), optional on an approve -- a note on an accepted video is stored as the item's
    // verdict reason just like a rejection's (maintainer request 2026-09-08). An empty string
    // is never sent; blank on an approval means no note.
    ...(reason ? { reason } : {}),
    row_version: rowVersion,
    // Only on an approve. A rejection sends the work back to be recorded again, so a value typed
    // before she changed her mind must not land on a record about to be redone. The backend drops
    // it too; sending it would just be a value the contract does not ask for.
    ...(decision === "approved" && measurementRead.measurement ? { measurement: measurementRead.measurement } : {}),
  };

  // Idempotency identity for this exact verdict payload. A double-click or browser retry resends
  // the same body with the same key; changing the now-user-authored approval note changes the key
  // too, matching the backend fingerprint that includes `reason`.
  const idempotencyKey = `verification-verdict-${itemId}-${rowVersion}-${decision}-${verdictRequestFingerprint(request)}`;
  const result = await recordVerificationVerdict(
    itemId,
    request,
    idempotencyKey,
  );
  revalidateVaccinationViews();
  if (!result.ok) {
    if (result.error.code === VARIANCE_CONFIRM_CODE) {
      // THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09). The backend refused the
      // approve ONCE because one or more packed weights sit more than 500 g from the plan; nothing
      // was written and row_version is unchanged. The drawer remounts on this redirect, so the
      // flagged keys AND the values she typed ride the URL: without them she would face empty boxes
      // and a banner about numbers that are gone. Only a direction code travels -- never a figure.
      const flagged = (result.error.fieldErrors ?? [])
        .filter((fe) => fe.field.startsWith(VARIANCE_FIELD_PREFIX))
        .map((fe) => `${fe.field.slice(VARIANCE_FIELD_PREFIX.length)}:${fe.code}`);
      const typed = (measurementRead.measurement?.entries ?? []).map((entry) => `${entry.key}:${entry.value}`);
      const bounced = withFeedback(url, "error", VARIANCE_CONFIRM_CODE);
      const withDetail = new URL(bounced, "http://local");
      if (flagged.length > 0) withDetail.searchParams.set("va_fields", flagged.join(","));
      if (typed.length > 0) withDetail.searchParams.set("va_entries", typed.join(","));
      redirect(`${withDetail.pathname}?${withDetail.searchParams.toString()}`);
    }
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
