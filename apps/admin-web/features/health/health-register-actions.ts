"use server";

import {
  discardHealthConfigRegisterDraft,
  openHealthConfigRegisterDraft,
  publishHealthConfigRegisterDraft,
  saveHealthConfigRegisterDraft,
  type HealthConfigFieldError,
  type HealthRegisterDocument,
  type HealthRegisterProblem,
} from "@/lib/api/server";

// =================================================================================================
// Diagnosis register writes — the other half of the Health Config rulebook.
//
// RULE 1 — NOTHING HERE INVENTS A CLINICAL VALUE, and the register makes that sharper than the
// treatment protocols do. A finding token is a JOIN between a question and a rule: normalising one
// ("Nasal_Discharge" -> "nasal_discharge", a stray space trimmed away) would silently connect or
// disconnect a rule from the answer that fires it. Every token is forwarded exactly as typed, and
// the backend's two-direction check is what tells the author it does not line up.
//
// RULE 2 — FIELD ERRORS COME BACK WHOLE. The backend returns every problem from one validation
// pass, each naming its own path (`questions.3.options.1.emits.0`). This layer passes that list
// through untouched. A register with forty questions rejected one field per round trip is not
// authorable — and the two directions usually fail in pairs, because an author adding a symptom
// and the rule that reads it gets both halves wrong at once or neither.
//
// RULE 3 — THESE ACTIONS RETURN A RESULT; THEY NEVER REVALIDATE THE ROUTE.
//
// A server action may hand the caller a result to apply in place, or it may redirect — never both.
// Revalidating on top of a returned result re-renders the route underneath a client that is still
// holding the answer, which on this screen would throw away an editor full of unsaved edits the
// moment the author pressed save. The editor decides what to do with what it gets back: refresh in
// place after a save, navigate after a publish or a discard.
//
// RULE 4 — WARNINGS SURVIVE A SUCCESS. A publish that went through still carries what it allowed:
// a question no rule reads yet, an illness whose treatment course nobody has written. They are
// returned on the ok path rather than dropped, because an author who is not told what they
// published cannot fix it later.
// =================================================================================================

export type HealthRegisterActionResult = {
  ok: boolean;
  /** A copy key in the health-config page contract; the client resolves it through `copy()`. */
  messageKey: string;
  detail?: string;
  code?: string;
  fieldErrors?: HealthConfigFieldError[];
  /** Non-fatal problems the publish allowed through. */
  warnings?: HealthRegisterProblem[];
  outcome?: string;
  /** The version this write left in place, for the caller to navigate to. */
  versionId?: string;
};

function failureKeyFor(code: string | undefined): string {
  switch (code) {
    case "draft_exists":
      return "error.draft_exists";
    case "not_a_draft":
      return "error.not_a_draft";
    case "invalid_register":
      return "error.validation_title";
    case "not_found":
      return "error.stale_version";
    default:
      return "action.error_backend";
  }
}

/**
 * The 422 body carries an `errors` array alongside the standard envelope. The generated error type
 * does not model that extra field, so it is read defensively — a missing or malformed array
 * degrades to "no field errors" and the summary still shows, rather than throwing inside a server
 * action where the failure would reach the author as a blank screen.
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

function warningsFrom(raw: HealthRegisterProblem[] | null | undefined): HealthRegisterProblem[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const nonFatal = raw.filter((p) => p && p.fatal === false);
  return nonFatal.length > 0 ? nonFatal : undefined;
}

/** Open the editor on a class's register, creating the draft from the live one when none is open. */
export async function openRegisterDraft(
  animalClass: string,
  idempotencyKey: string,
): Promise<HealthRegisterActionResult> {
  const result = await openHealthConfigRegisterDraft(animalClass, idempotencyKey);
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      code: result.error.code,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  return { ok: true, messageKey: "action.register_opened", versionId: result.data.register_version_id };
}

/**
 * Save the whole document — the form and the rules together.
 *
 * They are ONE request because they are one statement. Saving the questions and then the rules
 * would leave a window in which the draft says a rule reads a finding nothing produces, and that is
 * precisely the state the two-direction check exists to make unpublishable.
 */
export async function saveRegisterDraft(
  animalClass: string,
  document: HealthRegisterDocument,
  idempotencyKey: string,
): Promise<HealthRegisterActionResult> {
  const result = await saveHealthConfigRegisterDraft(
    { animal_class: animalClass, document },
    idempotencyKey,
  );
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      code: result.error.code,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  return {
    ok: true,
    // "unchanged" is a real outcome, not a silent no-op: the author pressed save and nothing
    // differed from what was already stored, and saying so is better than a success that looks
    // identical to one that wrote something.
    messageKey: result.data.outcome === "unchanged" ? "action.register_unchanged" : "action.register_saved",
    outcome: result.data.outcome,
    versionId: result.data.register_version_id,
  };
}

/** Publish a draft, retiring the register it replaces. */
export async function publishRegisterDraft(
  registerVersionId: string,
  idempotencyKey: string,
): Promise<HealthRegisterActionResult> {
  const result = await publishHealthConfigRegisterDraft(registerVersionId, idempotencyKey);
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      code: result.error.code,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  return {
    ok: true,
    messageKey: "action.register_published",
    outcome: result.data.outcome,
    versionId: result.data.register_version_id,
    warnings: warningsFrom(result.data.warnings),
  };
}

/** Discard a draft without publishing it. */
export async function discardRegisterDraft(
  registerVersionId: string,
  idempotencyKey: string,
): Promise<HealthRegisterActionResult> {
  const result = await discardHealthConfigRegisterDraft(registerVersionId, idempotencyKey);
  if (!result.ok) {
    return {
      ok: false,
      messageKey: failureKeyFor(result.error.code),
      detail: result.error.message,
      code: result.error.code,
      fieldErrors: fieldErrorsFrom(result.error),
    };
  }
  return { ok: true, messageKey: "action.register_discarded", outcome: result.data.outcome };
}
