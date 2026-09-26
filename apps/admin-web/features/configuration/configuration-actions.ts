"use server";

// Server Actions for /configuration/items (maintainer instruction 2026-09-18): add a row to a
// register, change one, archive / restore one, delete one. Every write goes to the backend under
// its own permission (configuration.write on the route table); this file only shapes the form
// and reports the outcome.
//
// EVERY ACTION LANDS IN PLACE (the market-config rule, maintainer report 2026-09-15): the outcome
// is RETURNED to the form that posted it (`useActionState` in row-drawer), beside the row being
// edited. Nothing navigates.
//
// This file composes no visible sentence of its own. `code` is a SUFFIX of a page-copy key the
// drawer resolves through the backend contract; `detail` is the backend's OWN refusal sentence
// carried through verbatim when it sent one; `fields` are the backend's per-field refusals, each
// shown under the input it names.

import { randomUUID } from "node:crypto";

import {
  createConfigurationRow,
  deleteConfigurationRow,
  setConfigurationRowStatus,
  updateConfigurationRow,
  type ConfigurationRowWrite,
} from "@/lib/api/configuration-server";
import { forgetAdminWebBootstrap, type ApiResult } from "@/lib/api/server";

export type ConfigurationActionState = {
  status: "idle" | "success" | "error";
  /** Suffix of `action.<code>` in the page copy: `success_message`, `deleted_message` or `failed_message`. */
  code: string;
  /** The backend's own sentence for a refusal, verbatim; empty when it sent none. */
  detail: string;
  /** The backend's per-field refusals, keyed by field. */
  fields: Record<string, string>;
  /** Bumps on every outcome so the same code twice still re-announces. */
  ticket: number;
};

function outcome(previous: ConfigurationActionState, result: ApiResult<unknown>, successCode = "success_message"): ConfigurationActionState {
  const ticket = previous.ticket + 1;
  if (!result.ok) {
    const detail = result.error.status !== undefined && result.error.status < 500 ? result.error.message : "";
    const fields: Record<string, string> = {};
    for (const entry of result.error.fieldErrors ?? []) fields[entry.field] = entry.message;
    return { status: "error", code: "failed_message", detail, fields, ticket };
  }
  // A saved row can be an option on another screen (a species in Register animal, a gender in
  // the Weights filter), so this process's cached contract is marked stale now rather than serving
  // the old list for up to its TTL.
  forgetAdminWebBootstrap();
  return { status: "success", code: successCode, detail: "", fields: {}, ticket };
}

function fieldsMissing(previous: ConfigurationActionState): ConfigurationActionState {
  return { status: "error", code: "error_form", detail: "", fields: {}, ticket: previous.ticket + 1 };
}

/** The drawer posts the whole draft as ONE JSON field so the decoder never re-derives column types. */
function decodeWrite(formData: FormData): { register: string; rowId: string; body: ConfigurationRowWrite } | null {
  const register = String(formData.get("register") ?? "").trim();
  const rowId = String(formData.get("row_id") ?? "").trim();
  const raw = formData.get("fields_json");
  if (!register || typeof raw !== "string") return null;
  let fields: unknown;
  try {
    fields = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!fields || typeof fields !== "object" || Array.isArray(fields)) return null;
  const rowVersion = Number(formData.get("row_version") ?? 0);
  return { register, rowId, body: { fields: fields as Record<string, unknown>, row_version: Number.isFinite(rowVersion) ? rowVersion : 0 } };
}

export async function createRowAction(previous: ConfigurationActionState, formData: FormData): Promise<ConfigurationActionState> {
  const write = decodeWrite(formData);
  if (!write) return fieldsMissing(previous);
  return outcome(previous, await createConfigurationRow(write.register, write.body, randomUUID()));
}

export async function updateRowAction(previous: ConfigurationActionState, formData: FormData): Promise<ConfigurationActionState> {
  const write = decodeWrite(formData);
  if (!write || !write.rowId) return fieldsMissing(previous);
  return outcome(previous, await updateConfigurationRow(write.register, write.rowId, write.body, randomUUID()));
}

export async function setRowStatusAction(previous: ConfigurationActionState, formData: FormData): Promise<ConfigurationActionState> {
  const register = String(formData.get("register") ?? "").trim();
  const rowId = String(formData.get("row_id") ?? "").trim();
  const status = String(formData.get("status") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? 0);
  if (!register || !rowId || (status !== "active" && status !== "archived")) return fieldsMissing(previous);
  return outcome(previous, await setConfigurationRowStatus(register, rowId, { status, row_version: Number.isFinite(rowVersion) ? rowVersion : 0 }, randomUUID()));
}

export async function deleteRowAction(previous: ConfigurationActionState, formData: FormData): Promise<ConfigurationActionState> {
  const register = String(formData.get("register") ?? "").trim();
  const rowId = String(formData.get("row_id") ?? "").trim();
  const rowVersion = Number(formData.get("row_version") ?? 0);
  if (!register || !rowId) return fieldsMissing(previous);
  return outcome(previous, await deleteConfigurationRow(register, rowId, Number.isFinite(rowVersion) ? rowVersion : 0, randomUUID()), "deleted_message");
}
