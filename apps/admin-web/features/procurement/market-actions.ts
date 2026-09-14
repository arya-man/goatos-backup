"use server";

// Server actions for the MARKET SURVEY config on /sales/config (maintainer decision 2026-09-14):
// the cities phoned each morning and the questions asked in each. Every write goes to the backend
// with its own permission (sales.market.config.write); this file only reports the outcome.

import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";

import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import {
  createMarketCity,
  createMarketQuestion,
  setMarketCallTime,
  updateMarketCity,
  updateMarketQuestion,
} from "@/lib/api/market-server";
import type { ApiResult } from "@/lib/api/server";

const SALES_CONFIG_PATH = "/sales/config";

function statusField(formData: FormData): "active" | "retired" {
  return optionalString(formData, "status") === "retired" ? "retired" : "active";
}

/** A duplicate name is the one refusal worth its own sentence; everything else is "check the fields". */
function failureKey(result: ApiResult<unknown>): string {
  if (!result.ok && result.error.code === "duplicate_name") return "action.market_duplicate";
  return "action.market_save_failed";
}

export async function addMarketCityAction(formData: FormData): Promise<void> {
  // A fresh key per submit: a retry of THIS submit cannot add the city twice.
  const result = await createMarketCity({ name: requiredString(formData, "name") }, randomUUID());
  if (!result.ok) actionRedirect(formData, "error", failureKey(result));
  revalidatePath(SALES_CONFIG_PATH);
  actionRedirect(formData, "success", "action.market_city_saved");
}

export async function updateMarketCityAction(formData: FormData): Promise<void> {
  const result = await updateMarketCity(requiredString(formData, "city_id"), {
    name: requiredString(formData, "name"),
    status: statusField(formData),
  });
  if (!result.ok) actionRedirect(formData, "error", failureKey(result));
  revalidatePath(SALES_CONFIG_PATH);
  actionRedirect(formData, "success", "action.market_city_saved");
}

export async function addMarketQuestionAction(formData: FormData): Promise<void> {
  const result = await createMarketQuestion(
    { label: requiredString(formData, "label"), unit_label: requiredString(formData, "unit_label") },
    randomUUID(),
  );
  if (!result.ok) actionRedirect(formData, "error", failureKey(result));
  revalidatePath(SALES_CONFIG_PATH);
  actionRedirect(formData, "success", "action.market_question_saved");
}

export async function updateMarketQuestionAction(formData: FormData): Promise<void> {
  const result = await updateMarketQuestion(requiredString(formData, "question_id"), {
    label: requiredString(formData, "label"),
    unit_label: requiredString(formData, "unit_label"),
    status: statusField(formData),
  });
  if (!result.ok) actionRedirect(formData, "error", failureKey(result));
  revalidatePath(SALES_CONFIG_PATH);
  actionRedirect(formData, "success", "action.market_question_saved");
}

export async function setMarketCallTimeAction(formData: FormData): Promise<void> {
  const result = await setMarketCallTime(requiredString(formData, "call_time"));
  if (!result.ok) actionRedirect(formData, "error", failureKey(result));
  revalidatePath(SALES_CONFIG_PATH);
  actionRedirect(formData, "success", "action.market_call_time_saved");
}
