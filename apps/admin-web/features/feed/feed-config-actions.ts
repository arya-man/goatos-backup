"use server";

import { revalidatePath } from "next/cache";

import {
  createFeedConfigFeedItem,
  setFeedConfigExperimentShedStatus,
  setFeedConfigFeedItemStatus,
  upsertFeedConfigExperiment,
  upsertFeedConfigExperimentBatch,
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

// -------------------------------------------------------------------------------------------------
// Feed items (the catalog)
//
// RULE 1 IS INVERTED HERE, AND THE INVERSION IS THE POINT.
//
// Everywhere else on this screen a blank numeric field is REJECTED, because a missing quantity is a
// blocking state that must never be filled in. On a catalog entry the four attributes are genuinely
// optional and a blank is a legitimate authored statement — "nobody has measured this" — so a blank
// is OMITTED FROM THE REQUEST rather than rejected, and stored as NULL.
//
//     blank        = not measured. Stored NULL. Blocks a nutritional rollup, nothing else.
//     explicit 0   = measured as zero. A different fact, and it is preserved as one.
//
// What does NOT change is that a blank is never turned into a 0, and an out-of-range value is never
// clamped — both are the same discipline as above, reaching the same conclusion from the other side.
//
// RULE 4 — ADDING AN ITEM AUTHORS NO QUANTITY.
//
// This action creates a NAME. It does not write a ration rate, a shed factor or an experiment cell,
// and it must never be extended to do so as a convenience: seeding a rate for the new item would
// author a number nobody entered, and seeding 0 would record "feed none of it" for every ration
// group and shed tag in the tenant. The revalidations below are what make the new item appear in
// the catalog list and in every feed-item picker; the ration grid correctly shows nothing for it
// until someone authors a rate.
// -------------------------------------------------------------------------------------------------

const FEED_ITEM_REJECTED = "action.feed_item_rejected";
const FEED_ITEM_SAVED = "action.feed_item_saved";
const FEED_ITEM_STATUS_CHANGED = "action.feed_item_status_changed";

/**
 * Reads an OPTIONAL authored attribute.
 *
 * Returns `undefined` for a cleared field — the caller OMITS it, recording "not measured" — and
 * `NaN` for a non-numeric one. An explicit "0" returns 0, which is a measured zero and a different
 * statement from leaving the box empty.
 */
function readOptionalNumber(formData: FormData, field: string): number | undefined {
  const raw = formData.get(field);
  if (typeof raw !== "string") return undefined;
  const trimmed = raw.trim();
  if (trimmed === "") return undefined; // not measured — NOT zero, and not rejected either.
  return Number(trimmed);
}

export async function saveFeedItem(formData: FormData): Promise<FeedConfigActionResult> {
  const feedItem = readRequiredText(formData, "feed_item");
  // The name is the one REQUIRED field, and its own message: "rejected, correct the values" would
  // not tell an operator who simply left the box empty what to do.
  if (!feedItem) return { ok: false, messageKey: "reason.feed_item_name_required" };

  const energy = readOptionalNumber(formData, "energy_kcal_per_kg");
  const dryMatter = readOptionalNumber(formData, "dry_matter_factor");
  const wastage = readOptionalNumber(formData, "wastage_factor");
  const displayOrder = readOptionalNumber(formData, "display_order");
  // Only a value that is not a number at all is rejected locally — there is nothing to send. Every
  // out-of-range value goes to the backend verbatim so its field error is what the operator reads.
  for (const value of [energy, dryMatter, wastage, displayOrder]) {
    if (value !== undefined && Number.isNaN(value)) {
      return { ok: false, messageKey: FEED_ITEM_REJECTED };
    }
  }

  const result = await createFeedConfigFeedItem(
    {
      feed_item: feedItem,
      // Each attribute is spread in ONLY when the operator typed one. Sending `null` would also
      // store NULL today, but omission is the honest wire shape for "the author said nothing about
      // this", and it keeps the door open for a future edit path where explicit null means "clear
      // the value I previously recorded".
      ...(energy === undefined ? {} : { energy_kcal_per_kg: energy }),
      ...(dryMatter === undefined ? {} : { dry_matter_factor: dryMatter }),
      ...(wastage === undefined ? {} : { wastage_factor: wastage }),
      ...(displayOrder === undefined ? {} : { display_order: displayOrder }),
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    // A duplicate name is its own explanation, not a generic rejection: the operator's next move is
    // to look for the item that already exists, not to re-type what they entered.
    const duplicate = result.error.code === "feed_item_exists";
    return {
      ok: false,
      messageKey: duplicate ? "reason.feed_item_exists" : FEED_ITEM_REJECTED,
      detail: duplicate ? undefined : result.error.message,
    };
  }

  revalidatePath("/feed/config");
  // Only /feed/config. Unlike every other write here, this one changes no quantity, so tomorrow's
  // direction and pack list are the same documents they were a moment ago — revalidating them would
  // imply the sheet moved when it did not.
  return { ok: true, messageKey: FEED_ITEM_SAVED };
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

/**
 * Enrol ONE PEN onto the experiment workflow, authoring every feed item of it in a single write.
 *
 * REPLACES the old shed-level, one-item-at-a-time enroller, which had three defects at once: it
 * offered SHEDS (so a new pen of an already-enrolled shed — Godel 1 - Part 8 — was unreachable), it
 * derived its candidate list from the current paginated cell page (so a pen configured on page 2
 * looked unconfigured on page 1), and it authored exactly one feed item, which is not what enrolling
 * a pen means. Candidates now come from the pen catalog endpoint, which states per pen whether it is
 * already configured.
 *
 * ONE ATOMIC WRITE. The quantities go through /feed-config/experiment/batch as a single transaction:
 * either the pen gets all of them or none. Posting N single-cell writes could half-succeed and leave
 * the pen ENROLLED — membership is the workflow flag — while fed only a subset of what was entered,
 * which is worse than not enrolling it at all.
 *
 * FIELDS ARE READ BY INDEXED NAME, NOT BY POSITION. Each row contributes `item_label_<i>` and
 * `item_kg_<i>`, paired by their shared index rather than by two `getAll()` arrays. Parallel arrays
 * are the grain bug this codebase keeps paying for: one array filtered and the other not shifts
 * every index and pairs a quantity with the wrong feed item — here, silently feeding a pen the wrong
 * thing.
 */
export async function enrolExperimentPen(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const penRaw = readRequiredText(formData, "pen");
  const category = readRequiredText(formData, "experiment_category");
  if (!parkId || !penRaw || !category) {
    return { ok: false, messageKey: REJECTED };
  }

  // The pen select carries shed id and raw partition label as one JSON value. A delimiter would be
  // unsafe: a partition label is free text ("Part 3"), so any separator could appear inside it.
  let shedId = "";
  let partitionLabel = "";
  try {
    const parsed: unknown = JSON.parse(penRaw);
    if (typeof parsed !== "object" || parsed === null) return { ok: false, messageKey: REJECTED };
    const pen = parsed as { s?: unknown; p?: unknown };
    if (typeof pen.s !== "string" || pen.s.trim() === "") return { ok: false, messageKey: REJECTED };
    // Absent `p` is a real value — an undivided shed — and must stay distinguishable from a bad one.
    if (pen.p !== undefined && typeof pen.p !== "string") return { ok: false, messageKey: REJECTED };
    shedId = pen.s.trim();
    partitionLabel = (pen.p ?? "").toString().trim();
  } catch {
    return { ok: false, messageKey: REJECTED };
  }

  const headCount = readOptionalCount(formData, "head_count");
  if (headCount !== undefined && Number.isNaN(headCount)) {
    return { ok: false, messageKey: REJECTED };
  }

  const items: { feed_item: string; absolute_kg: number }[] = [];
  for (let i = 0; ; i += 1) {
    const label = formData.get(`item_label_${i}`);
    if (typeof label !== "string") break;
    const trimmedLabel = label.trim();
    // A blank kg authors NOTHING for that item — the rule-1 blank-is-not-zero contract, applied per
    // row. It is skipped rather than sent as 0, which would mean "feed none of this, deliberately".
    const kg = readAuthoredNumber(formData, `item_kg_${i}`);
    if (kg === null) continue;
    if (Number.isNaN(kg) || trimmedLabel === "") return { ok: false, messageKey: REJECTED };
    items.push({ feed_item: trimmedLabel, absolute_kg: kg });
  }
  // Enrolling with no quantity would put the pen on the experiment workflow with nothing authored,
  // and the direction generator would then feed it nothing at all.
  if (items.length === 0) return EXPERIMENT_BLANK_IS_NOT_ZERO;

  const result = await upsertFeedConfigExperimentBatch(
    {
      park_id: parkId,
      shed_id: shedId,
      partition_label: partitionLabel,
      head_count: headCount ?? null,
      experiment_category: category,
      items,
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

export async function saveExperimentCell(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const shedId = readRequiredText(formData, "shed_id");
  const feedItem = readRequiredText(formData, "feed_item");
  const category = readRequiredText(formData, "experiment_category");
  if (!parkId || !shedId || !feedItem || !category) {
    return { ok: false, messageKey: REJECTED };
  }
  // NOT readRequiredText: an undivided shed authors a blank pen legitimately, so blank must reach
  // the backend as "the whole-shed row" rather than being rejected as a missing field.
  const partitionLabel = (formData.get("partition_label") ?? "").toString().trim();

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
      // Identifies WHICH PEN is being authored. Without it the write lands on the shed-wide row and
      // the author's number never reaches the pen they edited.
      partition_label: partitionLabel,
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
 * Move ONE PEN onto or off the experiment workflow.
 *
 * This is the switch the maintainer asked to be explicit: it is not a filter or a display toggle. An
 * active pen is fed the absolute kg authored for it; a retired one is fed from the ration grid
 * again. The status is read as a literal and validated against the two legal values rather than
 * being inferred from a checkbox — an unparsed value must never fall through to a default, because
 * both defaults would change what a pen's animals eat.
 *
 * PEN-SCOPED since 2026-08-09. The write used to be shed-wide while this screen was already
 * pen-grouped, so the button captioned "Return Godel 1 - Part 3" retired all ten Godel 1 pens. The
 * partition is sent verbatim and NOT defaulted when absent: a blank label is a real value meaning
 * "undivided shed", so it cannot be distinguished from a missing one here — the backend validates
 * it against the shed's catalog and rejects a blank on a subdivided shed rather than guessing.
 */
export async function setExperimentShedStatus(formData: FormData): Promise<FeedConfigActionResult> {
  const parkId = readRequiredText(formData, "park_id");
  const shedId = readRequiredText(formData, "shed_id");
  const status = readRequiredText(formData, "status");
  // Optional by shape, meaningful when blank: an undivided shed legitimately has no pen.
  const partitionRaw = formData.get("partition_label");
  const partitionLabel = typeof partitionRaw === "string" ? partitionRaw.trim() : "";
  if (!parkId || !shedId || (status !== "active" && status !== "retired")) {
    return { ok: false, messageKey: REJECTED };
  }

  const result = await setFeedConfigExperimentShedStatus(
    {
      park_id: parkId,
      shed_id: shedId,
      partition_label: partitionLabel,
      status,
    },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  // The switch changes which planner owns the pen, so tomorrow's direction and pack list are both
  // different documents now.
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: EXPERIMENT_SWITCHED };
}

/**
 * Retires one feed item, or restores a retired one.
 *
 * The status is validated against the closed pair here as well as server-side, because a value that
 * is neither would otherwise travel to the backend as a rejected write the operator sees as a
 * generic failure. There is no default: one value keeps the item in every feed sheet and the other
 * removes it from all of them.
 */
export async function setFeedItemStatus(formData: FormData): Promise<FeedConfigActionResult> {
  const feedItemId = readRequiredText(formData, "feed_item_id");
  const status = readRequiredText(formData, "status");
  if (!feedItemId || (status !== "active" && status !== "retired")) {
    return { ok: false, messageKey: FEED_ITEM_REJECTED };
  }

  const result = await setFeedConfigFeedItemStatus(
    { feed_item_id: feedItemId, status },
    readIdempotencyKey(formData),
  );
  if (!result.ok) {
    return { ok: false, messageKey: FEED_ITEM_REJECTED, detail: result.error.message };
  }

  revalidatePath("/feed/config");
  // The catalog is TENANT-wide and generation reads only its active rows, so retiring an item
  // changes tomorrow's sheet and pack list for EVERY park, not just the one on screen.
  revalidatePath("/feed/direction");
  revalidatePath("/feed/packing");
  return { ok: true, messageKey: FEED_ITEM_STATUS_CHANGED };
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
