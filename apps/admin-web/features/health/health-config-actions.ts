"use server";

import { revalidatePath } from "next/cache";

import {
  createHealthConfigDisease,
  discardHealthConfigDraft,
  openHealthConfigDraft,
  publishHealthConfigDraft,
  saveHealthConfigDraft,
  type HealthConfigFieldError,
  type SaveHealthConfigDraftRequest,
} from "@/lib/api/server";

// =================================================================================================
// Health Config writes — the authored treatment rulebook.
//
// RULE 1 — NOTHING HERE INVENTS A CLINICAL VALUE.
//
// Every field is read as a STRING and forwarded as typed. No `Number(...) ?? 0`, no clamping, no
// rounding, no defaulting a blank dosage to anything. A dosage is an instruction an operator
// administers to an animal without re-deriving it, so a value the author did not type must never
// reach the database. Out-of-range and malformed values are sent verbatim so the BACKEND rejects
// them with a field error the author can see and fix — silently correcting one would author a
// dosage nobody entered.
//
// The one exception is `duration_days`, and it is an exception in the safe direction: a genuinely
// ABSENT value is omitted from the request entirely so the backend applies its declared default,
// while a present-but-wrong value is forwarded to be rejected. A blank input therefore never
// becomes 0.
//
// RULE 2 — FIELD ERRORS COME BACK WHOLE.
//
// The backend returns EVERY offending field from one validation pass, each naming `steps[i].field`.
// This layer passes that list through untouched so the editor can mark every bad row at once. A
// 28-step protocol rejected one field per round trip is not authorable.
//
// RULE 3 — IDEMPOTENCY KEYS ARE MINTED PER HUMAN INTENT, NOT PER RETRY.
//
// Each submit carries the key the client minted when the form was opened and reuses it across
// retries of that same submit; the client rotates it only after a confirmed success. That is what
// makes a network-failed save safe to retry: the replay returns the original result instead of
// publishing twice.
// =================================================================================================

export type HealthConfigActionResult = {
  ok: boolean;
  /**
   * A copy key in the health-config page contract. The client resolves it through `copy()`, so this
   * action never returns visible prose of its own.
   */
  messageKey: string;
  /** Backend-authored error detail (already backend-owned text), shown verbatim when present. */
  detail?: string;
  /** Per-field errors from the backend's validation pass, in its order. */
  fieldErrors?: HealthConfigFieldError[];
  /** The write outcome (created | saved | unchanged | published | discarded) on success. */
  outcome?: string;
};

const HEALTH_CONFIG_PATH = "/health/config";

/** Maps a backend error envelope onto a contract copy key the page owns. */
function failureKeyFor(code: string | undefined): string {
  switch (code) {
    case "draft_exists":
      return "error.draft_exists";
    case "disease_exists":
      return "error.disease_exists";
    case "not_a_draft":
      return "error.not_a_draft";
    case "protocol_in_use":
      return "error.protocol_in_use";
    case "invalid_protocol":
      return "error.validation_title";
    default:
      return "action.error_backend";
  }
}

/**
 * The backend's 422 body carries an `errors` array alongside the standard envelope. The generated
 * error type does not model that extra field (it is a superset of ErrorEnvelope), so it is read
 * defensively here rather than cast — a missing or malformed array degrades to "no field errors"
 * and the summary message still shows, instead of throwing inside a server action.
 */
function fieldErrorsFrom(error: unknown): HealthConfigFieldError[] | undefined {
  if (!error || typeof error !== "object") return undefined;
  const raw = (error as { errors?: unknown }).errors;
  if (!Array.isArray(raw)) return undefined;
  const parsed = raw.filter(
    (entry): entry is HealthConfigFieldError =>
      Boolean(entry) &&
      typeof entry === "object" &&
      typeof (entry as HealthConfigFieldError).field === "string" &&
      typeof (entry as HealthConfigFieldError).message === "string",
  );
  return parsed.length > 0 ? parsed : undefined;
}

function readString(formData: FormData, name: string): string {
  const value = formData.get(name);
  return typeof value === "string" ? value.trim() : "";
}

/**
 * Reads a whole-number field WITHOUT ever turning a blank into a value.
 *
 * Returns `undefined` for a genuinely blank input (the caller omits the field so the backend
 * default applies) and the parsed number otherwise — including a number outside the allowed range,
 * which is forwarded so the backend rejects it. `Number("")` is 0, so the blank check must come
 * first and must not be replaced by a coercion or a `?? 0`.
 */
function readOptionalInt(formData: FormData, name: string): number | undefined | "invalid" {
  const raw = readString(formData, name);
  if (raw === "") return undefined;
  if (!/^-?\d+$/.test(raw)) return "invalid";
  return Number(raw);
}

export async function createDisease(formData: FormData): Promise<HealthConfigActionResult> {
  const displayName = readString(formData, "display_name");
  const idempotencyKey = readString(formData, "idempotency_key");
  const duration = readOptionalInt(formData, "duration_days");
  if (duration === "invalid") {
    return { ok: false, messageKey: "action.error_form" };
  }

  const result = await createHealthConfigDisease(
    {
      display_name: displayName,
      // Omitted entirely when blank — that is what makes the backend apply its declared default
      // rather than receiving a 0 this layer invented.
      ...(duration === undefined ? {} : { duration_days: duration }),
    },
    idempotencyKey || undefined,
  );
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  revalidatePath(HEALTH_CONFIG_PATH);
  return { ok: true, messageKey: "action.success_message", outcome: result.data.outcome };
}

/** Opens (or returns) the draft for one protocol, then re-renders the page with it selected. */
export async function openDraft(formData: FormData): Promise<HealthConfigActionResult> {
  const diseaseKey = readString(formData, "disease_key");
  const ageBand = readString(formData, "age_band");
  if (ageBand !== "adult" && ageBand !== "kid") {
    return { ok: false, messageKey: "action.error_form" };
  }
  const result = await openHealthConfigDraft({ disease_key: diseaseKey, age_band: ageBand });
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  revalidatePath(HEALTH_CONFIG_PATH);
  return { ok: true, messageKey: "action.success_message" };
}

/**
 * Saves a draft's whole content.
 *
 * The step list arrives as a JSON string in one form field rather than as indexed inputs. That is
 * deliberate: the editor owns add / remove / reorder, and a save is defined as "the document as the
 * author last saw it". Reconstructing that from `steps[3][medicine_name]`-style keys would make a
 * dropped or renamed key silently omit a step from a medical document — the accept-and-discard
 * failure mode. A malformed payload is rejected here with nothing sent.
 */
export async function saveDraft(formData: FormData): Promise<HealthConfigActionResult> {
  const diseaseKey = readString(formData, "disease_key");
  const ageBand = readString(formData, "age_band");
  const displayName = readString(formData, "display_name");
  const idempotencyKey = readString(formData, "idempotency_key");
  const duration = readOptionalInt(formData, "duration_days");
  if (duration === "invalid" || (ageBand !== "adult" && ageBand !== "kid")) {
    return { ok: false, messageKey: "action.error_form" };
  }

  let steps: SaveHealthConfigDraftRequest["steps"];
  try {
    const parsed: unknown = JSON.parse(readString(formData, "steps") || "[]");
    if (!Array.isArray(parsed)) throw new Error("steps must be an array");
    steps = parsed as SaveHealthConfigDraftRequest["steps"];
  } catch {
    return { ok: false, messageKey: "action.error_form" };
  }

  const result = await saveHealthConfigDraft(
    {
      disease_key: diseaseKey,
      age_band: ageBand,
      display_name: displayName,
      ...(duration === undefined ? {} : { duration_days: duration }),
      steps,
    },
    idempotencyKey || undefined,
  );
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  revalidatePath(HEALTH_CONFIG_PATH);
  return { ok: true, messageKey: "action.success_message", outcome: result.data.outcome };
}

export async function publishDraft(formData: FormData): Promise<HealthConfigActionResult> {
  const versionId = readString(formData, "protocol_version_id");
  const idempotencyKey = readString(formData, "idempotency_key");
  const result = await publishHealthConfigDraft(versionId, idempotencyKey || undefined);
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  revalidatePath(HEALTH_CONFIG_PATH);
  return { ok: true, messageKey: "action.success_message", outcome: result.data.outcome };
}

export async function discardDraft(formData: FormData): Promise<HealthConfigActionResult> {
  const versionId = readString(formData, "protocol_version_id");
  const idempotencyKey = readString(formData, "idempotency_key");
  const result = await discardHealthConfigDraft(versionId, idempotencyKey || undefined);
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  revalidatePath(HEALTH_CONFIG_PATH);
  return { ok: true, messageKey: "action.success_message", outcome: result.data.outcome };
}
