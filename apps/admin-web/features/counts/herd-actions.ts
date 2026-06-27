"use server";

// Counts -> Herd Register write flows. The real business entry point for the vaccination cascade:
// createAdminGoat / bulk-commit write canonical goat identity + emit goat.created, which generates
// vaccination obligations. Every call goes through a generated admin-api client (lib/api/server.ts) with an
// Idempotency-Key. No hand-rolled DTOs, no fake rows, no local route handlers, no client-only mutation.
import { randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import {
  actionErrorMessage,
  actionRedirect,
  optionalString,
  requiredString,
} from "@/lib/action-helpers";
import {
  commitAdminGoatBulkImport,
  createAdminGoat,
  previewAdminGoatBulkImport,
  type AdminGoatBulkResponse,
  type ApiResult,
  type CreateAdminGoatRequest,
} from "@/lib/api/server";

const SEXES = ["female", "male", "unknown"] as const;
const ORIGIN_TYPES = ["birth", "procured", "imported", "unknown"] as const;
const EVIDENCE_TYPES = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"] as const;

const HERD_PATH = "/counts/herd";

function inEnum<T extends string>(value: string | undefined, allowed: readonly T[], field: string): T {
  if (!value || !allowed.includes(value as T)) {
    throw new Error(`${field} must be one of: ${allowed.join(", ")}`);
  }
  return value as T;
}

function optFloat(formData: FormData, key: string): number | undefined {
  const raw = optionalString(formData, key);
  if (raw === undefined) return undefined;
  const n = Number.parseFloat(raw);
  if (!Number.isFinite(n) || n < 0) {
    throw new Error(`${key} must be a non-negative number`);
  }
  return n;
}

// Provenance is mandatory on a clean create (backend rejects an empty evidence_refs). If the operator did
// not supply a specific source reference, anchor it to this registration's idempotency key so the audit
// trail always points back to the exact admin action — never a fabricated id.
function evidenceRefs(formData: FormData, idempotencyKey: string): CreateAdminGoatRequest["evidence_refs"] {
  const evidenceId = optionalString(formData, "evidence_id") ?? `admin-register:${idempotencyKey}`;
  const evidenceType = inEnum(optionalString(formData, "evidence_type") ?? "source_record", EVIDENCE_TYPES, "evidence_type");
  const ref: CreateAdminGoatRequest["evidence_refs"][number] = { evidence_type: evidenceType, evidence_id: evidenceId };
  const description = optionalString(formData, "evidence_description");
  if (description) ref.description = description;
  return [ref];
}

export async function createGoatAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.goat_registered_no_generation";
  try {
    const idempotencyKey = optionalString(formData, "idempotency_key") ?? randomUUID();

    const rfid = optionalString(formData, "rfid");
    const oldTag = optionalString(formData, "old_tag");
    const tempFieldId = optionalString(formData, "temp_field_id");
    if (!rfid && !oldTag && !tempFieldId) {
      throw new Error("At least one identifier is required: RFID, old tag, or a temporary field id.");
    }

    const body: CreateAdminGoatRequest = {
      rfid,
      old_tag: oldTag,
      temp_field_id: tempFieldId,
      park_id: requiredString(formData, "park_id"),
      shed_id: requiredString(formData, "shed_id"),
      farm_id: optionalString(formData, "farm_id"),
      breed: optionalString(formData, "breed"),
      sex: inEnum(optionalString(formData, "sex") ?? "unknown", SEXES, "sex"),
      dob: optionalString(formData, "dob"),
      dob_estimated: formData.get("dob_estimated") === "on",
      origin_type: inEnum(optionalString(formData, "origin_type") ?? "unknown", ORIGIN_TYPES, "origin_type"),
      entry_date: requiredString(formData, "entry_date"),
      weight_kg: optFloat(formData, "weight_kg"),
      dam_id: optionalString(formData, "dam_id"),
      sire_or_lot: optionalString(formData, "sire_or_lot"),
      evidence_refs: evidenceRefs(formData, idempotencyKey),
    };

    const result = await createAdminGoat(body, idempotencyKey);
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey =
        result.data.generation_status === "queued"
          ? "action.goat_registered_generation_queued"
          : result.data.generation_status === "skipped_needs_review"
            ? "action.goat_registered_identity_review"
            : result.data.generation_status === "skipped_ineligible"
              ? "action.goat_registered_ineligible"
              : "action.goat_registered_no_generation";
      revalidatePath(HERD_PATH);
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

// Imperative server actions for the bulk drawer. They RETURN the backend response so the client drawer can
// show real row-level decisions/errors between preview and commit. They do not redirect; the client holds
// the (real) preview payload as transient UI state, never fabricated rows.
export async function previewGoatsAction(csv: string, fileHash: string): Promise<ApiResult<AdminGoatBulkResponse>> {
  return previewAdminGoatBulkImport({ csv, file_hash: fileHash });
}

export async function commitGoatsAction(
  rows: CreateAdminGoatRequest[],
  fileHash: string,
): Promise<ApiResult<AdminGoatBulkResponse>> {
  const result = await commitAdminGoatBulkImport({ rows, file_hash: fileHash }, randomUUID());
  if (result.ok && result.data.summary.created > 0) {
    revalidatePath(HERD_PATH);
  }
  return result;
}
