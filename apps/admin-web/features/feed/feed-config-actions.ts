"use server";

import { revalidatePath } from "next/cache";

import {
  setFeedConfigExperimentShedStatus,
  upsertFeedConfigExperiment,
  upsertFeedConfigRationRate,
  upsertFeedConfigSchedule,
  upsertFeedConfigShedFactor,
} from "@/lib/api/server";

// =================================================================================================
// Feed Config writes. Two rules govern every function here.
//
// RULE 1 — A CLEARED FIELD IS NOT ZERO, AND IS NEVER TURNED INTO ONE.
//
// `grams_per_head` and `multiplier` are numeric, which makes the fatal mistake trivially easy to
// make: `Number("")` is 0, so a single unguarded coercion converts "the operator cleared this field"
// into "the operator authored zero grams". Those two mean opposite things —
//
//     no authored rate  = NOT CONFIGURED. The shed is BLOCKED and will not be fed at all.
//     an authored 0     = feed nothing of this item. Correct and deliberate (K0/K1 kids on milk).
//
// — so a blank silently becoming 0 would take a shed that should be flagged as unconfigured and
// quietly record that it is meant to eat nothing, permanently, with no gap left for anyone to find.
//
// So every numeric field is read as a STRING and checked for blank BEFORE any numeric conversion. A
// blank is rejected outright with the contract's own explanation ("reason.blank_is_not_zero") and no
// request is sent. Only a non-blank value is converted, and an explicit "0" passes through as a real
// authored 0. Do not replace these parses with `Number(formData.get(...))` or a `?? 0` default.
//
// RULE 2 — OUT-OF-RANGE VALUES GO TO THE BACKEND VERBATIM.
//
// Nothing here clamps, rounds, or rewrites a value into range. A negative rate or an over-precise
// one is forwarded exactly as typed so the backend rejects it with a field error the operator can
// see. Silently correcting it would author a value nobody entered — the same class of bug as rule 1.
// The only local rejection is a value that is not a number at all, which has nothing to send.
//
// The writes are effective-dated server-side (an earlier day's row is CLOSED and a new one opened,
// so past feed sheets stay explainable) and idempotent: each submit carries a fresh Idempotency-Key,
// because each submit is a distinct human intent, while an exact replay of one returns the original
// result without re-running side effects.
// =================================================================================================

export type FeedConfigActionResult = {
  ok: boolean;
  /**
   * A copy key in the feed-config page contract. The client resolves it through `copy()`, so this
   * action never returns visible prose of its own.
   */
  messageKey: string;
  /** Backend-authored error detail (already backend-owned text), shown verbatim when present. */
  detail?: string;
};

const BLANK_IS_NOT_ZERO: FeedConfigActionResult = {
  ok: false,
  messageKey: "reason.blank_is_not_zero",
};
const REJECTED = "action.rate_rejected";
const SAVED = "action.rate_saved";

/**
 * The blank-is-not-zero rejection, worded for the experiment section.
 *
 * Same rule as BLANK_IS_NOT_ZERO, different consequence — which is why it is a different message.
 * On the ration grid, leaving a cell unconfigured BLOCKS the shed and the gap is visible. Here it is
 * silent: the shed simply stops being fed its authored kg.
 */
const EXPERIMENT_BLANK_IS_NOT_ZERO: FeedConfigActionResult = {
  ok: false,
  messageKey: "reason.experiment_blank_is_not_zero",
};
const EXPERIMENT_SAVED = "action.experiment_saved";
const EXPERIMENT_SWITCHED = "action.experiment_switched";

/**
 * Reads a numeric form field WITHOUT ever coercing blank to a number.
 *
 * Returns `null` for a cleared field (caller must reject, not default) and `NaN` for a non-numeric
 * one. An explicit "0" returns 0, which is a legitimate authored value.
 */
function readAuthoredNumber(formData: FormData, field: string): number | null {
  const raw = formData.get(field);
  if (typeof raw !== "string") return null;
  const trimmed = raw.trim();
  if (trimmed === "") return null; // cleared — NOT zero.
  return Number(trimmed);
}

function readRequiredText(formData: FormData, field: string): string {
  const raw = formData.get(field);
  return typeof raw === "string" ? raw.trim() : "";
}

/**
 * Reads the idempotency key minted client-side when the editing form opened (see
 * FeedConfigFormShell in feed-config-editor.tsx). Every `upsertFeedConfig*` call below passes this
 * through EXPLICITLY rather than relying on that function's default-parameter fallback: the
 * default mints a brand-new key on every call, so a resubmit of the same open form (e.g. after a
 * lost/ambiguous response) would silently send a different key and risk a second write. The
 * fallback here exists only for a malformed/missing hidden field, which should not happen from the
 * real form.
 */
function readIdempotencyKey(formData: FormData): string | undefined {
  const raw = formData.get("idempotency_key");
  if (typeof raw !== "string") return undefined;
  const trimmed = raw.trim();
  return trimmed === "" ? undefined : trimmed;
}

export async function saveRationRate(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const rationGroup = readRequiredText(formData, "ration_group");
  const shedTag = readRequiredText(formData, "shed_tag");
  const feedItem = readRequiredText(formData, "feed_item");
  if (!parkId || !rationGroup || !shedTag || !feedItem) {
    return { ok: false, messageKey: REJECTED };
  }

  const gramsPerHead = readAuthoredNumber(formData, "grams_per_head");
  // Blank: the operator cleared the field. That leaves the combination UNCONFIGURED (and therefore
  // blocked) — it does not author zero, and no request is sent.
  if (gramsPerHead === null) return BLANK_IS_NOT_ZERO;
  if (Number.isNaN(gramsPerHead)) return { ok: false, messageKey: REJECTED };

  const result = await upsertFeedConfigRationRate(
    {
      park_id: parkId,
      ration_group: rationGroup,
      shed_tag: shedTag,
      feed_item: feedItem,
      // Sent verbatim. A negative or over-precise value is the backend's to reject.
      grams_per_head: gramsPerHead,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  // A rate change changes tomorrow's generated sheet, and every shed that resolved to this key.
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: SAVED };
}

export async function saveShedFactor(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const shedId = readRequiredText(formData, "shed_id");
  const feedItem = readRequiredText(formData, "feed_item");
  if (!parkId || !shedId || !feedItem) {
    return { ok: false, messageKey: REJECTED };
  }

  const multiplier = readAuthoredNumber(formData, "multiplier");
  // Blank here is just as dangerous in the other direction: a shed with NO factor row is treated as
  // 1.0 by the read path, so inventing a value for an empty field would author a multiplier nobody
  // entered. An explicit 0 IS accepted — it is a deliberate authored value.
  if (multiplier === null) return BLANK_IS_NOT_ZERO;
  if (Number.isNaN(multiplier)) return { ok: false, messageKey: REJECTED };

  const result = await upsertFeedConfigShedFactor(
    {
      park_id: parkId,
      shed_id: shedId,
      feed_item: feedItem,
      multiplier,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: SAVED };
}

// -------------------------------------------------------------------------------------------------
// Experiment sheds
//
// RULE 1 APPLIES HERE TOO, AND THE STAKES ARE HIGHER, NOT LOWER.
//
// `Number("")` is 0 in exactly the same way, but the failure is quieter than on the ration grid:
//
//     no ration rate      = BLOCKED. Visible gap. Someone gets a blocked shed to fix.
//     no experiment row   = the shed silently stops being an experiment shed and is fed
//                           head count x grams per head x shed factor off the grid instead.
//
// That second state prints a complete-looking feed sheet with the wrong arithmetic on it; measured
// against the source workbook for 2026-07-20 it was 398.8 kg of CBE concentrate instead of 182.0 kg.
// So the same discipline holds: read every numeric field as a STRING, reject blanks before any
// conversion, and forward out-of-range values verbatim for the backend to refuse.
//
// RULE 3 — THE HEAD COUNT IS NEVER A MULTIPLIER, AND IS NEVER INVENTED.
//
// `absolute_kg` is already the shed total. `head_count` is informational context and is passed
// through untouched; a cleared one is sent as null ("not recorded"), which is a different statement
// from 0 ("this shed is empty"). Nothing here multiplies, defaults, or derives it.
// -------------------------------------------------------------------------------------------------

/**
 * Reads the optional informational head count.
 *
 * Returns `undefined` for a cleared field — the caller sends `null`, recording "not recorded" — and
 * `NaN` for a non-numeric one. It is NEVER defaulted to 0, which would state the shed is empty.
 */
function readOptionalCount(formData: FormData, field: string): number | undefined {
  const raw = formData.get(field);
  if (typeof raw !== "string") return undefined;
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  return Number(trimmed);
}

export async function saveExperimentCell(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const shedId = readRequiredText(formData, "shed_id");
  const feedItem = readRequiredText(formData, "feed_item");
  const category = readRequiredText(formData, "experiment_category");
  if (!parkId || !shedId || !feedItem || !category) {
    return { ok: false, messageKey: REJECTED };
  }

  const absoluteKg = readAuthoredNumber(formData, "absolute_kg");
  // Blank: the operator cleared the field. That is not "feed nothing" and not "leave it alone" — no
  // request is sent, and the contract's own explanation is returned.
  if (absoluteKg === null) return EXPERIMENT_BLANK_IS_NOT_ZERO;
  if (Number.isNaN(absoluteKg)) return { ok: false, messageKey: REJECTED };

  const headCount = readOptionalCount(formData, "head_count");
  if (headCount !== undefined && Number.isNaN(headCount)) {
    return { ok: false, messageKey: REJECTED };
  }

  const result = await upsertFeedConfigExperiment(
    {
      park_id: parkId,
      shed_id: shedId,
      feed_item: feedItem,
      // Sent verbatim. A negative or over-precise value is the backend's to reject.
      absolute_kg: absoluteKg,
      // A cleared count records "not recorded" as null. It is never coerced to 0, and it is never
      // multiplied into absolute_kg by anything on this path.
      head_count: headCount ?? null,
      experiment_category: category,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: EXPERIMENT_SAVED };
}

/**
 * Move a whole shed onto or off the experiment workflow.
 *
 * This is the switch the maintainer asked to be explicit: it is not a filter or a display toggle. An
 * active shed is fed the absolute kg authored for it; a retired one is fed from the ration grid
 * again. The status is read as a literal and validated against the two legal values rather than
 * being inferred from a checkbox — an unparsed value must never fall through to a default, because
 * both defaults would change what a shed's animals eat.
 */
export async function setExperimentShedStatus(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const shedId = readRequiredText(formData, "shed_id");
  const status = readRequiredText(formData, "status");
  if (!parkId || !shedId || (status !== "active" && status !== "retired")) {
    return { ok: false, messageKey: REJECTED };
  }

  const result = await setFeedConfigExperimentShedStatus(
    {
      park_id: parkId,
      shed_id: shedId,
      status,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  // The switch changes which planner owns the shed, so tomorrow's direction and pack list are both
  // different documents now.
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: EXPERIMENT_SWITCHED };
}

export async function saveSchedule(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const workflow = readRequiredText(formData, "workflow");
  const directionTime = readRequiredText(formData, "direction_time");
  const correctionTime = readRequiredText(formData, "correction_time");
  const transportRaw = formData.get("transport_time");
  const transportTime = typeof transportRaw === "string" ? transportRaw.trim() : "";

  if (!parkId || (workflow !== "normal" && workflow !== "experiment")) {
    return { ok: false, messageKey: REJECTED };
  }
  // direction_time and correction_time are required by the contract; a cleared one is a blank, not a
  // midnight default.
  if (!directionTime || !correctionTime) return BLANK_IS_NOT_ZERO;

  const result = await upsertFeedConfigSchedule(
    {
      park_id: parkId,
      workflow,
      // Wall-clock strings pass through untouched: they are recurring Asia/Kolkata business-calendar
      // rules, not instants, and attaching an offset here would corrupt them.
      direction_time: directionTime,
      correction_time: correctionTime,
      // A cleared transport time is the explicit "no declared cutoff" the contract allows as null. A
      // malformed one is sent as typed so the backend rejects it — turning a typo into null would tell
      // the dispatcher this park has no transport deadline at all.
      transport_time: transportTime === "" ? null : transportTime,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: SAVED };
}
