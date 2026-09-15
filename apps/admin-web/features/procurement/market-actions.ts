"use server";

// Server actions for the MARKET SURVEY config on /sales/config (maintainer decision 2026-09-14):
// the cities phoned each morning and the questions asked in each. Every write goes to the backend
// with its own permission (sales.market.config.write); this file only reports the outcome.
//
// EVERY ACTION LANDS IN PLACE (maintainer report 2026-09-15: "when I add any city or make any
// change the whole page is loading and I am going to top"). The first version ended each action
// with a redirect back to the page carrying `?notice=`, which is a full navigation: the page
// re-rendered from scratch and the scroll position went with it. Now each action RETURNS its
// outcome to the form that posted it (`useActionState` in market-config-section), and the
// `revalidatePath` re-reads the section's server data inside the same response, so the new city
// or the renamed question appears where the reader is looking and nothing moves.

import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";

import { optionalString, requiredString } from "@/lib/action-helpers";
import {
  createMarketCity,
  createMarketQuestion,
  setMarketCallTime,
  updateMarketCity,
  updateMarketQuestion,
} from "@/lib/api/market-server";
import type { ApiResult } from "@/lib/api/server";

const SALES_CONFIG_PATH = "/sales/config";

/**
 * The outcome of one market-config write, returned to the form that posted it. `code` is a
 * SUFFIX of a page-copy key (`action.<code>`): the section resolves it through the backend
 * contract, so no visible sentence is composed here. `ticket` bumps on every outcome so the same
 * code twice still re-announces.
 */
export type MarketActionState = {
  status: "idle" | "success" | "error";
  code: string;
  ticket: number;
};

function statusField(formData: FormData): "active" | "retired" {
  return optionalString(formData, "status") === "retired" ? "retired" : "active";
}

/** A duplicate name is the one refusal worth its own sentence; everything else is "check the fields". */
function failureCode(result: ApiResult<unknown>): string {
  if (!result.ok && result.error.code === "duplicate_name") return "market_duplicate";
  return "market_save_failed";
}

function outcome(
  previous: MarketActionState,
  result: ApiResult<unknown>,
  successCode: string,
): MarketActionState {
  const ticket = previous.ticket + 1;
  if (!result.ok) return { status: "error", code: failureCode(result), ticket };
  // Re-read the section's server data inside this same response: the list updates in place.
  revalidatePath(SALES_CONFIG_PATH);
  return { status: "success", code: successCode, ticket };
}

/** A missing required field is reported like any other refusal, never thrown at the reader. */
function fieldsMissing(previous: MarketActionState): MarketActionState {
  return { status: "error", code: "market_save_failed", ticket: previous.ticket + 1 };
}

export async function addMarketCityAction(previous: MarketActionState, formData: FormData): Promise<MarketActionState> {
  let name: string;
  try {
    name = requiredString(formData, "name");
  } catch {
    return fieldsMissing(previous);
  }
  // A fresh key per submit: a retry of THIS submit cannot add the city twice.
  return outcome(previous, await createMarketCity({ name }, randomUUID()), "market_city_saved");
}

export async function updateMarketCityAction(previous: MarketActionState, formData: FormData): Promise<MarketActionState> {
  let cityId: string;
  let name: string;
  try {
    cityId = requiredString(formData, "city_id");
    name = requiredString(formData, "name");
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(previous, await updateMarketCity(cityId, { name, status: statusField(formData) }), "market_city_saved");
}

export async function addMarketQuestionAction(previous: MarketActionState, formData: FormData): Promise<MarketActionState> {
  let label: string;
  let unitLabel: string;
  try {
    label = requiredString(formData, "label");
    unitLabel = requiredString(formData, "unit_label");
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(
    previous,
    await createMarketQuestion({ label, unit_label: unitLabel }, randomUUID()),
    "market_question_saved",
  );
}

export async function updateMarketQuestionAction(previous: MarketActionState, formData: FormData): Promise<MarketActionState> {
  let questionId: string;
  let label: string;
  let unitLabel: string;
  try {
    questionId = requiredString(formData, "question_id");
    label = requiredString(formData, "label");
    unitLabel = requiredString(formData, "unit_label");
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(
    previous,
    await updateMarketQuestion(questionId, { label, unit_label: unitLabel, status: statusField(formData) }),
    "market_question_saved",
  );
}

export async function setMarketCallTimeAction(previous: MarketActionState, formData: FormData): Promise<MarketActionState> {
  let callTime: string;
  try {
    callTime = requiredString(formData, "call_time");
  } catch {
    return fieldsMissing(previous);
  }
  return outcome(previous, await setMarketCallTime(callTime), "market_call_time_saved");
}
