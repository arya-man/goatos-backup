import type { VaccinationOperationsProtocol } from "@/lib/api/server";

const TEST_ONLY_LABEL_RE = /\s*\(test-only local development\)\s*/gi;
const SEED_SUFFIX_RE = /\s*(?:-|\u2013)\s*seed_[a-z0-9]+-[a-z0-9_]+/gi;
const DOSE_RE = /\b(primary|booster[\s_-]?\d*|annual|catch[\s_-]?up)\b/i;
const DOSE_STRIP_RE = /(?:^|[\s_-]+)(primary|booster[\s_-]?\d*|annual|catch[\s_-]?up)(?:[\s_-]*\d+)?(?=$|[\s_-]+)/gi;
const VACCINE_LABELS: Record<string, string> = {
  BLUE_TONGUE: "Blue Tongue",
  ETTT: "ET+TT",
  ET_TT: "ET+TT",
  FMD: "FMD",
  GOAT_POX: "Goat Pox",
  HS: "HS",
  PPR: "PPR",
  SHEEP_POX: "Sheep Pox",
};

function normalizedProtocolName(protocol: VaccinationOperationsProtocol): string {
  return protocol.name.toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

function cleanDisplayLabel(raw: string): string {
  return raw
    .replace(TEST_ONLY_LABEL_RE, " ")
    .replace(SEED_SUFFIX_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}

function compactDisplayLabel(raw: string): string {
  const cleaned = cleanDisplayLabel(raw);
  if (!cleaned) return "";
  return /^[a-z0-9]{2,5}$/i.test(cleaned) ? cleaned.toUpperCase() : cleaned;
}

function doseLabel(raw: string): string {
  const match = raw.match(DOSE_RE);
  return match
    ? match[1]
        .replace(/[\s_-]+/g, " ")
        .trim()
        .replace(/\b\w/g, (m) => m.toUpperCase())
    : "";
}

export function vaccinationProtocolDisplayName(protocol: VaccinationOperationsProtocol): string {
  return compactDisplayLabel(protocol.name) || normalizedProtocolName(protocol) || "Vaccination protocol";
}

// Clean a raw drive/protocol display string (e.g. "FMD (TEST-ONLY local development) - seed_fmd-PRIMARY")
// down to the canonical vaccine label + dose phase ("FMD · Primary"). Strips the TEST-ONLY/seed-code noise so
// the execution board doesn't render verbose fixture identifiers, consistent with the matrix column labels.
export function vaccinationDriveDisplayName(raw: string | undefined | null): string {
  const source = (raw ?? "").trim();
  if (!source) return "Vaccination drive";
  const dose = doseLabel(source);
  const labelWithoutDose = cleanDisplayLabel(source).replace(DOSE_STRIP_RE, " ").replace(/\s+/g, " ").trim();
  const label = compactDisplayLabel(labelWithoutDose);
  if (label && dose) return `${label} · ${dose}`;
  if (label) return label;
  if (dose) return dose;
  return compactDisplayLabel(source) || "Vaccination drive";
}

export function vaccinationVaccineDisplayName(raw: string | undefined | null): string {
  const source = cleanDisplayLabel(raw ?? "");
  if (!source) return "";
  const key = source.toUpperCase().replace(/[^A-Z0-9]+/g, "_").replace(/^_+|_+$/g, "");
  if (VACCINE_LABELS[key]) return VACCINE_LABELS[key];
  return compactDisplayLabel(source.replace(/[_-]+/g, " ").replace(/\s+/g, " "));
}

export function sortVaccinationProtocols(protocols: VaccinationOperationsProtocol[]): VaccinationOperationsProtocol[] {
  return [...protocols].sort((a, b) => vaccinationProtocolDisplayName(a).localeCompare(vaccinationProtocolDisplayName(b)));
}
