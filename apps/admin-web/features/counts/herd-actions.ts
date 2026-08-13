"use server";

// Counts -> Herd Register write flows. The real business entry point for the vaccination cascade:
// createAdminGoat / bulk-commit write canonical goat identity + emit goat.created, which generates
// vaccination obligations. Every call goes through a generated admin-api client (lib/api/server.ts) with an
// Idempotency-Key. No hand-rolled DTOs, no fake rows, no local route handlers, no client-only mutation.
import { createHash, randomUUID } from "node:crypto";
import { revalidatePath } from "next/cache";
import {
  actionErrorMessage,
  actionRedirect,
  optionalString,
  requiredString,
} from "@/lib/action-helpers";
import {
  commitAdminGoatBulkImport,
  createLocation,
  createAdminGoat,
  getGoatPassport,
  listLocations,
  previewAdminGoatBulkImport,
  reproductiveGoat,
  type AdminGoatBulkCommitRequest,
  type AdminGoatBulkResponse,
  type ApiResult,
  type CreateAdminGoatRequest,
  type CreateLocationRequest,
  type LocationMutationResponse,
  type LocationSummary,
  type ReproductiveGoatRequest,
} from "@/lib/api/server";
import { parseCSVRecords } from "./herd-import-utils";

const SEXES = ["female", "male"] as const;
const SPECIES = ["goat", "sheep"] as const;
const ORIGIN_TYPES = ["birth", "procured", "imported"] as const;
const EVIDENCE_TYPES = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"] as const;

const HERD_PATH = "/counts/herd";
const MAX_SHED_IMPORT_ROWS = 500;
const BULK_FILE_SHA256_RE = /^[a-f0-9]{64}$/;

export type ShedImportDecision = "create" | "requires_review";

export type ShedImportRowResult = {
  row_number: number;
  decision: ShedImportDecision;
  errors: { field: string; code: string; message: string }[];
  source_label?: string;
  normalized?: CreateLocationRequest;
  result?: LocationMutationResponse;
};

export type ShedImportResponse = {
  summary: {
    total: number;
    create_ready: number;
    requires_review: number;
    created: number;
    failed: number;
  };
  rows: ShedImportRowResult[];
};

export type ShedImportActionResult =
  | { ok: true; data: ShedImportResponse }
  | { ok: false; message: string };

export type ShedImportCommitRow = {
  row_number: number;
  normalized: CreateLocationRequest;
};

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

function optInt(formData: FormData, key: string): number | undefined {
  const raw = optionalString(formData, key);
  if (raw === undefined) return undefined;
  if (!/^\d+$/.test(raw)) {
    throw new Error(`${key} must be a non-negative integer`);
  }
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n) || n < 0) {
    throw new Error(`${key} must be a non-negative integer`);
  }
  return n;
}

function stableJSONValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stableJSONValue);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, v]) => v !== undefined)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([k, v]) => [k, stableJSONValue(v)]),
    );
  }
  return value;
}

function bulkCommitRowsHash(rows: unknown): string {
  return createHash("sha256").update(JSON.stringify(stableJSONValue(rows))).digest("hex");
}

function vaccinationShedOperational(displayOrder = 0, notes?: string | null): CreateLocationRequest["operational"] {
  return {
    usable_for_counts: true,
    usable_for_feed: false,
    usable_for_vaccination: true,
    usable_for_sop: true,
    is_holding: false,
    is_quarantine: false,
    is_icu: false,
    display_order: displayOrder,
    notes: notes ?? null,
  };
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

    const animalIdentifier1 = requiredString(formData, "animal_identifier_1");
    const animalIdentifier2 = optionalString(formData, "animal_identifier_2");
    const partitionLabel = optionalString(formData, "partition_label");
    if (animalIdentifier2 && animalIdentifier1.trim().toUpperCase() === animalIdentifier2.trim().toUpperCase()) {
      throw new Error("Animal ID 1 and Animal ID 2 must be different.");
    }

    const body: CreateAdminGoatRequest = {
      animal_identifier_1: animalIdentifier1,
      ...(animalIdentifier2 ? { animal_identifier_2: animalIdentifier2 } : {}),
      species: inEnum(optionalString(formData, "species"), SPECIES, "species"),
      park_id: requiredString(formData, "park_id"),
      shed_id: requiredString(formData, "shed_id"),
      ...(partitionLabel ? { partition_label: partitionLabel } : {}),
      farm_id: optionalString(formData, "farm_id"),
      breed: optionalString(formData, "breed"),
      management_stage: requiredString(formData, "management_stage"),
      sex: inEnum(optionalString(formData, "sex"), SEXES, "sex"),
      dob: requiredString(formData, "dob"),
      dob_estimated: formData.get("dob_estimated") === "on",
      origin_type: inEnum(optionalString(formData, "origin_type"), ORIGIN_TYPES, "origin_type"),
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
            ? "action.goat_registered_rejected"
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

export async function createShedAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.shed_registered";
  try {
    const idempotencyKey = optionalString(formData, "idempotency_key") ?? randomUUID();
    const displayOrder = optInt(formData, "display_order") ?? 0;
    const notes = optionalString(formData, "notes");
    const body: CreateLocationRequest = {
      location_type: "shed",
      location_code: optionalString(formData, "location_code") ?? null,
      name: requiredString(formData, "name"),
      parent_location_id: requiredString(formData, "park_id"),
      status: "active",
      country: "IN",
      timezone: "Asia/Kolkata",
      operational: vaccinationShedOperational(displayOrder, notes ?? null),
    };
    const result = await createLocation(body, idempotencyKey);
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      revalidatePath(HERD_PATH);
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

// Counts -> Herd Register reproductive edit. Records a reproductive status change on one goat. The value
// list is backend-owned (herd_reproductive option group compiled from active reproductive
// status_definitions); this action only forwards the operator's chosen key. row_version is read
// server-side from the current passport (herd list rows do not carry it), so a concurrent edit fails
// closed with a write conflict instead of silently clobbering. Idempotency-Key replays a double-submit.
export async function reproductiveGoatAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.reproductive_updated";
  try {
    const idempotencyKey = optionalString(formData, "idempotency_key") ?? randomUUID();
    const goatId = requiredString(formData, "goat_id");
    const reproductiveStatus = requiredString(formData, "reproductive_status");
    const reason = requiredString(formData, "reproductive_reason");
    const evidenceType = inEnum(optionalString(formData, "evidence_type") ?? "source_record", EVIDENCE_TYPES, "evidence_type");

    const passport = await getGoatPassport(goatId);
    if (!passport.ok) {
      status = "error";
      actionKey = actionErrorMessage(passport.error);
    } else {
      const body: ReproductiveGoatRequest = {
        reproductive_status: reproductiveStatus,
        reason,
        evidence_refs: [{ evidence_type: evidenceType, evidence_id: `admin-reproductive:${idempotencyKey}` }],
        row_version: passport.data.goat.row_version,
      };
      const breedingDate = optionalString(formData, "breeding_date");
      if (breedingDate) body.breeding_date = breedingDate;
      const lastDeliveryDate = optionalString(formData, "last_delivery_date");
      if (lastDeliveryDate) body.last_delivery_date = lastDeliveryDate;

      const result = await reproductiveGoat(goatId, body, idempotencyKey);
      if (!result.ok) {
        status = "error";
        actionKey = actionErrorMessage(result.error);
      } else {
        revalidatePath(HERD_PATH);
      }
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
  rows: AdminGoatBulkCommitRequest["rows"],
  fileHash: string,
  previewToken: string,
): Promise<ApiResult<AdminGoatBulkResponse>> {
  const stableFileHash = (typeof fileHash === "string" ? fileHash : "").trim().toLowerCase();
  if (!BULK_FILE_SHA256_RE.test(stableFileHash)) {
    return { ok: false, error: { kind: "bad_request", code: "invalid_file_hash", message: "Goat import commit received an invalid file hash; preview the CSV again." } };
  }
  const stablePreviewToken = (typeof previewToken === "string" ? previewToken : "").trim();
  if (stablePreviewToken === "") {
    return { ok: false, error: { kind: "bad_request", code: "invalid_preview_token", message: "Goat import commit is missing the preview token; preview the CSV again." } };
  }
  const commitHash = bulkCommitRowsHash(rows);
  const result = await commitAdminGoatBulkImport(
    { rows, file_hash: stableFileHash, preview_token: stablePreviewToken },
    `goat-bulk:${stableFileHash}:${commitHash}`,
  );
  if (result.ok && result.data.summary.created > 0) {
    revalidatePath(HERD_PATH);
  }
  return result;
}

export async function previewShedsAction(csv: string): Promise<ShedImportActionResult> {
  try {
    const records = parseCSV(csv);
    if (records.length < 2) {
      return { ok: false, message: "CSV must include a header and at least one row." };
    }
    const dataRecords = records
      .slice(1)
      .map((record, index) => ({ record, rowNumber: index + 2 }))
      .filter(({ record }) => !record.every((cell) => cell.trim() === ""));
    if (dataRecords.length === 0) {
      return { ok: false, message: "CSV must include a header and at least one row." };
    }
    if (dataRecords.length > MAX_SHED_IMPORT_ROWS) {
      return { ok: false, message: `Shed import supports at most ${MAX_SHED_IMPORT_ROWS} rows.` };
    }
    const parksResult = await listLocations({ type: "park", status: "active", limit: 500 });
    if (!parksResult.ok) {
      return { ok: false, message: `Parks unavailable: ${parksResult.error.message}` };
    }
    const headers = headerMap(records[0]);
    const parks = parksResult.data.items;
    const response: ShedImportResponse = { summary: { total: 0, create_ready: 0, requires_review: 0, created: 0, failed: 0 }, rows: [] };
    const seenSheds = new Map<string, number>();
    dataRecords.forEach(({ record, rowNumber }) => {
      const row = previewShedImportRow(record, rowNumber, headers, parks);
      if (row.decision === "create" && row.normalized) {
        const duplicate = duplicateShedImportError(row.normalized, rowNumber, seenSheds);
        if (duplicate) {
          row.decision = "requires_review";
          row.errors = [duplicate];
          row.normalized = undefined;
        }
      }
      if (row.decision === "create") response.summary.create_ready += 1;
      else response.summary.requires_review += 1;
      response.rows.push(row);
    });
    response.summary.total = response.rows.length;
    return { ok: true, data: response };
  } catch (error) {
    return { ok: false, message: error instanceof Error ? error.message : "CSV could not be parsed." };
  }
}

export async function commitShedsAction(rows: ShedImportCommitRow[], fileHash: string): Promise<ShedImportActionResult> {
  const stableFileHash = (typeof fileHash === "string" ? fileHash : "").trim().toLowerCase();
  if (!BULK_FILE_SHA256_RE.test(stableFileHash)) {
    return { ok: false, message: "Shed import commit received an invalid file hash; preview the CSV again." };
  }
  if (!Array.isArray(rows)) {
    return { ok: false, message: "Shed import commit received invalid rows; preview the CSV again." };
  }
  if (rows.length > MAX_SHED_IMPORT_ROWS) {
    return { ok: false, message: `Shed import supports at most ${MAX_SHED_IMPORT_ROWS} rows.` };
  }

  const parksResult = await listLocations({ type: "park", status: "active", limit: 500 });
  if (!parksResult.ok) {
    return { ok: false, message: `Parks unavailable: ${parksResult.error.message}` };
  }

  const commitHash = bulkCommitRowsHash(rows);
  const activeParkIDs = new Set(parksResult.data.items.map((park) => park.location_id));
  const seenRows = new Set<number>();
  const seenSheds = new Map<string, number>();
  const response: ShedImportResponse = { summary: { total: rows.length, create_ready: 0, requires_review: 0, created: 0, failed: 0 }, rows: [] };
  for (const row of rows) {
    const rowNumber = Number.isSafeInteger(row?.row_number) ? row.row_number : response.rows.length + 2;
    const commit = normalizeCommittedShedRow(row, activeParkIDs, seenRows);
    if (commit.errors.length > 0 || !commit.normalized) {
      response.summary.requires_review += 1;
      response.summary.failed += 1;
      response.rows.push({
        row_number: rowNumber,
        decision: "requires_review",
        errors: commit.errors,
        source_label: row?.normalized?.location_code ?? row?.normalized?.name,
        normalized: commit.normalized,
      });
      continue;
    }
    const duplicate = duplicateShedImportError(commit.normalized, rowNumber, seenSheds);
    if (duplicate) {
      response.summary.requires_review += 1;
      response.summary.failed += 1;
      response.rows.push({
        row_number: rowNumber,
        decision: "requires_review",
        errors: [duplicate],
        source_label: commit.normalized.location_code ?? commit.normalized.name,
        normalized: commit.normalized,
      });
      continue;
    }

    const idempotencyKey = `shed-bulk:${commitHash}:row:${rowNumber}`;
    // serial-await: allow creates are committed sequentially to keep import row side effects ordered.
    const result = await createLocation(commit.normalized, idempotencyKey);
    if (result.ok) {
      response.summary.created += 1;
      response.rows.push({
        row_number: rowNumber,
        decision: "create",
        errors: [],
        source_label: commit.normalized.location_code ?? commit.normalized.name,
        normalized: commit.normalized,
        result: result.data,
      });
    } else {
      response.summary.failed += 1;
      response.rows.push({
        row_number: rowNumber,
        decision: "requires_review",
        errors: [{ field: "row", code: result.error.code ?? result.error.kind, message: result.error.message }],
        source_label: commit.normalized.location_code ?? commit.normalized.name,
        normalized: commit.normalized,
      });
    }
  }
  if (response.summary.created > 0) {
    revalidatePath(HERD_PATH);
  }
  return { ok: true, data: response };
}

function previewShedImportRow(record: string[], rowNumber: number, headers: Map<string, number>, parks: LocationSummary[]): ShedImportRowResult {
  const errors: ShedImportRowResult["errors"] = [];
  const parkValue = valueCSV(record, headers, "park");
  const shedName = valueCSV(record, headers, "shed_name") || valueCSV(record, headers, "name");
  const shedCode = valueCSV(record, headers, "shed_code") || valueCSV(record, headers, "location_code");
  const sourceLabel = [shedCode, shedName].filter(Boolean).join(" · ") || undefined;
  const notes = valueCSV(record, headers, "notes");
  const displayOrderRaw = valueCSV(record, headers, "display_order");
  const park = resolvePark(parks, parkValue);
  if (!parkValue) errors.push({ field: "park", code: "required", message: "Park is required." });
  else if (!park) errors.push({ field: "park", code: "not_found", message: "Park must match an active park id, code, or name." });
  if (!shedName) errors.push({ field: "shed_name", code: "required", message: "Shed name is required." });
  if (shedName.length > 200) errors.push({ field: "shed_name", code: "too_long", message: "Shed name must be at most 200 characters." });
  if (shedCode.length > 80) errors.push({ field: "shed_code", code: "too_long", message: "Shed code must be at most 80 characters." });
  const displayOrder = displayOrderRaw ? Number.parseInt(displayOrderRaw, 10) : 0;
  if ((displayOrderRaw && !/^\d+$/.test(displayOrderRaw)) || !Number.isFinite(displayOrder) || displayOrder < 0) {
    errors.push({ field: "display_order", code: "invalid", message: "Display order must be a non-negative integer." });
  }
  let normalized: CreateLocationRequest | undefined;
  if (errors.length === 0 && park) {
    normalized = {
      location_type: "shed",
      location_code: shedCode || null,
      name: shedName,
      parent_location_id: park.location_id,
      status: "active",
      country: "IN",
      timezone: "Asia/Kolkata",
      operational: vaccinationShedOperational(displayOrder, notes || null),
    };
  }
  const decision: ShedImportDecision = errors.length === 0 ? "create" : "requires_review";
  return { row_number: rowNumber, decision, errors, source_label: sourceLabel, normalized };
}

function duplicateShedImportError(
  normalized: CreateLocationRequest,
  rowNumber: number,
  seen: Map<string, number>,
): ShedImportRowResult["errors"][number] | null {
  const keys = shedImportDuplicateKeys(normalized);
  for (const key of keys) {
    const firstRow = seen.get(key);
    if (firstRow !== undefined) {
      return { field: "shed", code: "duplicate_in_file", message: `Duplicate shed in uploaded sheet; first seen on row ${firstRow}.` };
    }
  }
  for (const key of keys) seen.set(key, rowNumber);
  return null;
}

function shedImportDuplicateKeys(normalized: CreateLocationRequest): string[] {
  const park = String(normalized.parent_location_id ?? "").trim().toLowerCase();
  const name = String(normalized.name ?? "").trim().toLowerCase();
  const code = String(normalized.location_code ?? "").trim().toLowerCase();
  const keys: string[] = [];
  if (park && code) keys.push(`${park}:code:${code}`);
  if (park && name) keys.push(`${park}:name:${name}`);
  return keys;
}

function normalizeCommittedShedRow(
  row: ShedImportCommitRow | null | undefined,
  activeParkIDs: Set<string>,
  seenRows: Set<number>,
): { normalized?: CreateLocationRequest; errors: ShedImportRowResult["errors"] } {
  const errors: ShedImportRowResult["errors"] = [];
  if (!row || typeof row !== "object") {
    errors.push({ field: "row", code: "invalid", message: "Commit row is missing the previewed shed payload." });
    return { errors };
  }
  if (!Number.isSafeInteger(row.row_number) || row.row_number < 2) {
    errors.push({ field: "row_number", code: "invalid", message: "Row number must be a CSV data row." });
  } else if (seenRows.has(row.row_number)) {
    errors.push({ field: "row_number", code: "duplicate", message: "Duplicate row number in commit payload." });
  } else {
    seenRows.add(row.row_number);
  }

  const source: Partial<CreateLocationRequest> | undefined = row.normalized;
  if (!source || typeof source !== "object") {
    errors.push({ field: "row", code: "invalid", message: "Commit row is missing the previewed shed payload." });
    return { errors };
  }
  const name = typeof source.name === "string" ? source.name.trim() : "";
  const locationCode = typeof source.location_code === "string" ? source.location_code.trim() : "";
  const parentLocationID = typeof source.parent_location_id === "string" ? source.parent_location_id.trim() : "";
  const op = source.operational;

  if (source.location_type !== "shed") {
    errors.push({ field: "location_type", code: "invalid", message: "Shed import can only create shed locations." });
  }
  if (source.status !== "active") {
    errors.push({ field: "status", code: "invalid", message: "Shed import can only create active sheds." });
  }
  if (!name) errors.push({ field: "shed_name", code: "required", message: "Shed name is required." });
  if (name.length > 200) errors.push({ field: "shed_name", code: "too_long", message: "Shed name must be at most 200 characters." });
  if (locationCode.length > 80) errors.push({ field: "shed_code", code: "too_long", message: "Shed code must be at most 80 characters." });
  if (!parentLocationID || !activeParkIDs.has(parentLocationID)) {
    errors.push({ field: "park", code: "not_found", message: "Park must still match an active park." });
  }
  if (!op) {
    errors.push({ field: "operational", code: "required", message: "Operational attributes are required for shed import commit." });
  } else {
    if (
      op.usable_for_counts !== true ||
      op.usable_for_feed !== false ||
      op.usable_for_vaccination !== true ||
      op.usable_for_sop !== true ||
      op.is_holding !== false ||
      op.is_quarantine !== false ||
      op.is_icu !== false
    ) {
      errors.push({ field: "operational", code: "invalid", message: "Shed import can only create vaccination-usable, non-feed, non-holding sheds." });
    }
    if (!Number.isSafeInteger(op.display_order) || op.display_order < 0) {
      errors.push({ field: "display_order", code: "invalid", message: "Display order must be a non-negative integer." });
    }
    if (op.notes !== null && typeof op.notes !== "string") {
      errors.push({ field: "notes", code: "invalid", message: "Notes must be text." });
    } else if (typeof op.notes === "string" && op.notes.length > 2000) {
      errors.push({ field: "notes", code: "too_long", message: "Notes must be at most 2000 characters." });
    }
  }

  if (errors.length > 0 || !op) {
    return { errors };
  }
  return {
    errors: [],
    normalized: {
      location_type: "shed",
      location_code: locationCode || null,
      name,
      parent_location_id: parentLocationID,
      status: "active",
      country: "IN",
      timezone: "Asia/Kolkata",
      operational: vaccinationShedOperational(op.display_order, op.notes),
    },
  };
}

function resolvePark(parks: LocationSummary[], raw: string): LocationSummary | undefined {
  const value = raw.trim().toLowerCase();
  if (!value) return undefined;
  return parks.find((park) => {
    const code = park.location_code?.toLowerCase() ?? "";
    return park.location_id.toLowerCase() === value || code === value || park.name.toLowerCase() === value;
  });
}

function parseCSV(raw: string): string[][] {
  return parseCSVRecords(raw, { trimCells: true });
}

function headerMap(header: string[]): Map<string, number> {
  const out = new Map<string, number>();
  header.forEach((value, index) => out.set(normalizeHeader(value), index));
  return out;
}

function normalizeHeader(value: string): string {
  const key = value.trim().toLowerCase().replace(/[./()]/g, "").replace(/[\s/-]+/g, "_");
  switch (key) {
    case "shed":
    case "shed_name":
    case "name":
      return "shed_name";
    case "shed_code":
    case "location_code":
      return "shed_code";
    default:
      return key;
  }
}

function valueCSV(record: string[], headers: Map<string, number>, key: string): string {
  const index = headers.get(key);
  if (index === undefined || index < 0 || index >= record.length) return "";
  return record[index].trim();
}
