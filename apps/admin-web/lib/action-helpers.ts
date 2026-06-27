import "server-only";

import { redirect } from "next/navigation";
import type { ApiUiError } from "@/lib/api/server";

export type EvidenceType = "source_record" | "identifier" | "goat" | "event" | "media" | "decision" | "import_run" | "conflict" | "location" | "actor";

export type EvidenceRefInput = {
  evidence_type: EvidenceType;
  evidence_id: string;
  source_system?: string | null;
  description?: string | null;
};

const evidenceTypes: EvidenceType[] = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"];

export function requiredString(formData: FormData, key: string): string {
  const value = stringField(formData, key);
  if (!value) {
    throw new Error(`${key} is required`);
  }
  return value;
}

export function optionalString(formData: FormData, key: string): string | undefined {
  return stringField(formData, key);
}

export function requiredNumber(formData: FormData, key: string): number {
  const value = requiredString(formData, key);
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 1) {
    throw new Error(`${key} must be a positive number`);
  }
  return parsed;
}

export function optionalBoolean(formData: FormData, key: string): boolean {
  return formData.get(key) === "on" || formData.get(key) === "true";
}

export function requiredEvidenceRef(formData: FormData): EvidenceRefInput[] {
  const type = requiredString(formData, "evidence_type");
  if (!evidenceTypes.includes(type as EvidenceType)) {
    throw new Error("evidence_type is not supported");
  }
  const ref: EvidenceRefInput = {
    evidence_type: type as EvidenceType,
    evidence_id: requiredString(formData, "evidence_id"),
  };
  const sourceSystem = optionalString(formData, "evidence_source_system");
  if (sourceSystem) ref.source_system = sourceSystem;
  const description = optionalString(formData, "evidence_description");
  if (description) ref.description = description;
  return [ref];
}

export function affectedGoatIDs(formData: FormData): string[] {
  return requiredString(formData, "affected_goat_ids")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function actionRedirect(formData: FormData, status: "success" | "error", actionKey: string): never {
  redirect(withActionFeedback(safeReturnTo(formData), status, actionKey));
}

export function actionErrorMessage(error: ApiUiError): string {
  void error;
  return "action.error_backend";
}

export function safeReturnTo(formData: FormData, fallback = "/"): string {
  const value = optionalString(formData, "return_to") ?? fallback;
  if (!value.startsWith("/") || value.startsWith("//")) return fallback;
  return value;
}

function withActionFeedback(path: string, status: "success" | "error", actionKey: string): string {
  const [pathWithoutHash, hash = ""] = path.split("#", 2);
  const [pathname, query = ""] = pathWithoutHash.split("?", 2);
  const params = new URLSearchParams(query);
  params.delete("action_status");
  params.delete("action_message");
  params.delete("action_key");
  params.set("action_status", status);
  params.set("action_key", actionKey.startsWith("action.") ? actionKey : "action.error_form");
  const qs = params.toString();
  const next = qs ? `${pathname}?${qs}` : pathname;
  return hash ? `${next}#${hash}` : next;
}

function stringField(formData: FormData, key: string): string | undefined {
  const value = formData.get(key);
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}
