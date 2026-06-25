"use server";

// Real operator write flows for procurement source entry. Every action calls a generated backend endpoint
// with a fresh Idempotency-Key, then redirects back to the originating page with an action banner. No fake
// success: the backend response (or error envelope) drives the message. The backend derives the actor from
// the auth token; bodies carry only operator-entered data.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import { actionErrorMessage, actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import {
  acceptProcurementIntake,
  addProcurementLoadGoat,
  createProcurementLoad,
  dispatchProcurementLoad,
  recordProcurementArrivalReview,
  recordProcurementPreDispatchDecision,
  recordProcurementSourceHealth,
} from "@/lib/api/procurement-server";
import type {
  AcceptProcurementIntakeRequest,
  AddProcurementLoadGoatRequest,
  CreateProcurementLoadRequest,
  DispatchProcurementLoadRequest,
  ProcurementArrivalGoatRequest,
  ProcurementArrivalState,
  ProcurementHealthState,
  ProcurementOwnershipState,
  ProcurementSelectionState,
  RecordProcurementArrivalReviewRequest,
  RecordProcurementDecisionRequest,
  RecordProcurementSourceHealthRequest,
} from "@/lib/api/procurement";

const DECISION_TYPES = ["accepted", "rejected", "deferred", "blocked"] as const;
const HEALTH_STATES = ["passed", "failed", "deferred"] as const;
const ARRIVAL_STATUSES = ["pending", "mismatch", "accepted", "rejected", "deferred", "blocked"] as const;
const ARRIVAL_STATES: readonly ProcurementArrivalState[] = [
  "matched", "missing", "extra_unresolved", "health_flag", "weight_flag", "accepted", "rejected", "deferred", "blocked",
];
const INTAKE_SIGNALS = ["clear", "defer", "quarantine", "review"] as const;
const SELECTION_STATES: ProcurementSelectionState[] = [
  "source_only", "candidate", "purchased", "accepted", "rejected", "deferred", "blocked",
  "loaded", "arrival_accepted", "arrival_rejected", "accepted_herd_intake", "dead", "sold", "lost",
];
const HEALTH_FULL: ProcurementHealthState[] = ["pending", "passed", "failed", "deferred"];
const OWNERSHIP_STATES: ProcurementOwnershipState[] = ["pending", "shared_pending", "mesha_owned", "blocked", "not_owned", "settled"];

function optInt(formData: FormData, key: string): number | undefined {
  const raw = optionalString(formData, key);
  if (raw === undefined) return undefined;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

// Browser <input type="datetime-local"> yields "2026-06-24T15:30" (no seconds, no zone), which is not
// RFC3339 and fails Go's *time.Time JSON decode. Normalize to a full RFC3339 instant before posting.
function optRfc3339(formData: FormData, key: string): string | null {
  const raw = optionalString(formData, key);
  if (!raw) return null;
  const d = new Date(raw);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

function csvIds(formData: FormData, key: string): string[] {
  const raw = optionalString(formData, key);
  if (!raw) return [];
  return raw.split(",").map((s) => s.trim()).filter(Boolean);
}

// Per-goat arrival rows, one per line: "<goat_id>[, <arrival_state>]" (state defaults to "accepted"). The
// backend only advances goats to arrival_accepted from these rows, so AcceptIntake stays blocked without them.
function arrivalGoatRows(formData: FormData, key: string): ProcurementArrivalGoatRequest[] {
  const raw = optionalString(formData, key);
  if (!raw) return [];
  return raw
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [goatId, state] = line.split(",").map((s) => s.trim());
      if (!goatId) {
        throw new Error("Each arrival goat row needs a goat id.");
      }
      return {
        goat_id: goatId,
        arrival_state: inEnum<ProcurementArrivalState>(state || "accepted", ARRIVAL_STATES, "arrival_state"),
      };
    });
}

function inEnum<T extends string>(value: string | undefined, allowed: readonly T[], field: string): T {
  if (!value || !allowed.includes(value as T)) {
    throw new Error(`${field} must be one of: ${allowed.join(", ")}`);
  }
  return value as T;
}

function revalidateProcurement(loadId?: string): void {
  // Procurement is operational source-entry only. Command-room screens are top-level (and not part of the
  // current vaccination-focused E2E), so a write here only revalidates the source-entry surfaces.
  revalidatePath("/procurement/source-entry");
  if (loadId) revalidatePath(`/procurement/source-entry/loads/${loadId}`);
}

export async function createLoadAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: CreateProcurementLoadRequest = {
      source_party_id: requiredString(formData, "source_party_id"),
      source_location_id: optionalString(formData, "source_location_id") ?? null,
      expected_count: optInt(formData, "expected_count"),
      purchase_date: optionalString(formData, "purchase_date") ?? null,
      planned_dispatch_at: optRfc3339(formData, "planned_dispatch_at"),
      notes: optionalString(formData, "notes"),
    };
    const result = await createProcurementLoad(body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Load ${result.data.load.load_id.slice(0, 8)} created.`;
      revalidateProcurement(result.data.load.load_id);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create load.";
  }
  actionRedirect(formData, status, message);
}

export async function addSourceGoatAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = requiredString(formData, "load_id");
  try {
    const body: AddProcurementLoadGoatRequest = {
      source_tag: optionalString(formData, "source_tag") ?? null,
      source_rfid: optionalString(formData, "source_rfid") ?? null,
      temporary_id: optionalString(formData, "temporary_id") ?? null,
      selection_state: optionalString(formData, "selection_state")
        ? inEnum<ProcurementSelectionState>(optionalString(formData, "selection_state"), SELECTION_STATES, "selection_state")
        : undefined,
      selection_reason: optionalString(formData, "selection_reason"),
      health_state: optionalString(formData, "health_state")
        ? inEnum<ProcurementHealthState>(optionalString(formData, "health_state"), HEALTH_FULL, "health_state")
        : undefined,
      ownership_state: optionalString(formData, "ownership_state")
        ? inEnum<ProcurementOwnershipState>(optionalString(formData, "ownership_state"), OWNERSHIP_STATES, "ownership_state")
        : undefined,
      warmup_days: optInt(formData, "warmup_days"),
      holding_location_id: optionalString(formData, "holding_location_id") ?? null,
    };
    if (!body.source_tag && !body.source_rfid && !body.temporary_id) {
      throw new Error("Provide at least one source identifier (source tag, RFID, or temporary id).");
    }
    const result = await addProcurementLoadGoat(loadId, body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = "Source goat added to load.";
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to add source goat.";
  }
  actionRedirect(formData, status, message);
}

export async function recordSourceHealthAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = optionalString(formData, "load_id");
  try {
    const body: RecordProcurementSourceHealthRequest = {
      load_id: requiredString(formData, "load_id"),
      health_state: inEnum(optionalString(formData, "health_state"), HEALTH_STATES, "health_state"),
      reason: optionalString(formData, "reason"),
    };
    const result = await recordProcurementSourceHealth(requiredString(formData, "goat_id"), body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Source health recorded: ${body.health_state}.`;
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to record source health.";
  }
  actionRedirect(formData, status, message);
}

export async function preDispatchDecisionAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = optionalString(formData, "load_id");
  try {
    const body: RecordProcurementDecisionRequest = {
      load_id: requiredString(formData, "load_id"),
      decision_type: inEnum(optionalString(formData, "decision_type"), DECISION_TYPES, "decision_type"),
      reason: optionalString(formData, "reason"),
      owner_id: optionalString(formData, "owner_id") ?? null,
      resume_condition: optionalString(formData, "resume_condition") ?? null,
    };
    const result = await recordProcurementPreDispatchDecision(requiredString(formData, "goat_id"), body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Pre-dispatch decision recorded: ${body.decision_type}.`;
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to record decision.";
  }
  actionRedirect(formData, status, message);
}

export async function dispatchLoadAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = requiredString(formData, "load_id");
  try {
    const body: DispatchProcurementLoadRequest = {
      to_location_id: requiredString(formData, "to_location_id"),
      from_location_id: optionalString(formData, "from_location_id") ?? null,
      goat_ids: csvIds(formData, "goat_ids"),
      dispatched_at: optRfc3339(formData, "dispatched_at"),
      proof_ref_id: optionalString(formData, "proof_ref_id") ?? null,
    };
    const result = await dispatchProcurementLoad(loadId, body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = "Dispatch / transit recorded.";
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to record dispatch.";
  }
  actionRedirect(formData, status, message);
}

export async function arrivalReviewAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = requiredString(formData, "load_id");
  try {
    const body: RecordProcurementArrivalReviewRequest = {
      park_location_id: requiredString(formData, "park_location_id"),
      expected_count: optInt(formData, "expected_count"),
      loaded_count: optInt(formData, "loaded_count"),
      arrived_count: optInt(formData, "arrived_count"),
      matched_count: optInt(formData, "matched_count"),
      missing_count: optInt(formData, "missing_count"),
      extra_count: optInt(formData, "extra_count"),
      rejected_count: optInt(formData, "rejected_count"),
      status: optionalString(formData, "status")
        ? inEnum(optionalString(formData, "status"), ARRIVAL_STATUSES, "status")
        : undefined,
      goats: arrivalGoatRows(formData, "goats"),
    };
    const result = await recordProcurementArrivalReview(loadId, body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = "Arrival review recorded.";
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to record arrival review.";
  }
  actionRedirect(formData, status, message);
}

export async function acceptIntakeAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let message = "";
  const loadId = requiredString(formData, "load_id");
  try {
    const body: AcceptProcurementIntakeRequest = {
      goat_ids: csvIds(formData, "goat_ids"),
      park_location_id: requiredString(formData, "park_location_id"),
      shed_location_id: requiredString(formData, "shed_location_id"),
      entry_date: optionalString(formData, "entry_date") ?? null,
      intake_health_signal: optionalString(formData, "intake_health_signal")
        ? inEnum(optionalString(formData, "intake_health_signal"), INTAKE_SIGNALS, "intake_health_signal")
        : null,
    };
    const result = await acceptProcurementIntake(loadId, body, randomUUID());
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Accepted intake — ${result.data.handoffs.length} PHC handoff${result.data.handoffs.length === 1 ? "" : "s"} created.`;
      revalidateProcurement(loadId);
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to accept intake.";
  }
  actionRedirect(formData, status, message);
}
