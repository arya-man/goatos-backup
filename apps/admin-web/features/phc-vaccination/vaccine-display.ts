import type { VaccinationOperationsProtocol } from "@/lib/api/server";

const VACCINE_COLUMNS = [
  { key: "ppr", label: "PPR" },
  { key: "fmd", label: "FMD" },
  { key: "enterotox", label: "ENTEROTOX" },
  { key: "deworm", label: "DEWORM" },
  { key: "ccpp", label: "CCPP" },
] as const;

function normalizedProtocolName(protocol: VaccinationOperationsProtocol): string {
  return protocol.name.toLowerCase().replace(/[^a-z0-9]+/g, " ");
}

function vaccineColumnKey(protocol: VaccinationOperationsProtocol): string {
  const normalized = normalizedProtocolName(protocol);
  if (normalized.includes("ppr")) return "ppr";
  if (normalized.includes("fmd")) return "fmd";
  if (normalized.includes("enterotox")) return "enterotox";
  if (normalized.includes("deworm")) return "deworm";
  if (normalized.includes("ccpp")) return "ccpp";
  return normalized.trim();
}

export function vaccinationProtocolDisplayName(protocol: VaccinationOperationsProtocol): string {
  const key = vaccineColumnKey(protocol);
  const column = VACCINE_COLUMNS.find((candidate) => candidate.key === key);
  return column?.label ?? protocol.name.replace(/\s*\(test-only local development\)\s*/gi, "").trim();
}

// Clean a raw drive/protocol display string (e.g. "FMD (TEST-ONLY local development) - seed_fmd-PRIMARY")
// down to the canonical vaccine label + dose phase ("FMD · Primary"). Strips the TEST-ONLY/seed-code noise so
// the execution board doesn't render verbose fixture identifiers, consistent with the matrix column labels.
export function vaccinationDriveDisplayName(raw: string | undefined | null): string {
  const source = (raw ?? "").trim();
  if (!source) return "Vaccination drive";
  const lower = source.toLowerCase();
  const column = VACCINE_COLUMNS.find((c) => lower.includes(c.key) || (c.key === "enterotox" && lower.includes("enterotox")));
  const doseMatch = source.match(/\b(primary|booster[\s_-]?\d*|annual|catch[\s_-]?up)\b/i);
  const dose = doseMatch
    ? doseMatch[1]
        .replace(/[\s_-]+/g, " ")
        .trim()
        .replace(/\b\w/g, (m) => m.toUpperCase())
    : "";
  if (column) return dose ? `${column.label} · ${dose}` : column.label;
  // No known vaccine keyword (e.g. "Trigger Gate PHC Vaccination"): strip only the TEST-ONLY/seed noise.
  const cleaned = source
    .replace(/\s*\(test-only local development\)\s*/gi, " ")
    .replace(/\s*[-–]\s*seed_[a-z0-9]+-[a-z0-9_]+/gi, "")
    .replace(/\s+/g, " ")
    .trim();
  return cleaned || "Vaccination drive";
}

export function sortVaccinationProtocols(protocols: VaccinationOperationsProtocol[]): VaccinationOperationsProtocol[] {
  return [...protocols].sort((a, b) => {
    const aKey = vaccineColumnKey(a);
    const bKey = vaccineColumnKey(b);
    const aIndex = VACCINE_COLUMNS.findIndex((column) => column.key === aKey);
    const bIndex = VACCINE_COLUMNS.findIndex((column) => column.key === bKey);
    if (aIndex !== -1 && bIndex !== -1) return aIndex - bIndex;
    if (aIndex !== -1) return -1;
    if (bIndex !== -1) return 1;
    return vaccinationProtocolDisplayName(a).localeCompare(vaccinationProtocolDisplayName(b));
  });
}
