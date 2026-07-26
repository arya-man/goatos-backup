type NamedProtocol = {
  name: string;
};

const TEST_ONLY_LABEL_RE = /\s*\(test-only local development\)\s*/gi;
const SEED_SUFFIX_RE = /\s*(?:-|\u2013)\s*seed_[a-z0-9]+-[a-z0-9_]+/gi;
const DOSE_RE = /\b(primary|booster[\s_-]?\d*|annual|catch[\s_-]?up)\b/i;
const DOSE_STRIP_RE = /(?:^|[\s_-]+)(primary|booster[\s_-]?\d*|annual|catch[\s_-]?up)(?:[\s_-]*\d+)?(?=$|[\s_-]+)/gi;
const MATRIX_CODE_RE = /\b(blue_tongue|goat_pox|sheep_pox|et_tt|fmd|ppr|hs)(?:_adult|_kid)?(?:_w\d+|_m\d+|_yr\d+|_primary|_booster\d*|_annual|_catch_up)?\b/i;
const MATRIX_PREFIX_RE = /^Preventive Care Vaccination Matrix\s*-?\s*/i;
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

const MATRIX_VACCINE_LABELS: Record<string, string> = {
  BLUE_TONGUE: "Blue Tongue",
  ET_TT: "ET+TT",
  FMD: "FMD",
  GOAT_POX: "Goat Pox",
  HS: "HS",
  PPR: "PPR",
  SHEEP_POX: "Sheep Pox",
};

function normalizedProtocolName(protocol: NamedProtocol): string {
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

function matrixScheduleLabel(raw: string): string {
  const match = raw.match(MATRIX_CODE_RE);
  if (!match) return "";
  const code = match[0].toLowerCase();
  const vaccine = MATRIX_VACCINE_LABELS[
    Object.keys(MATRIX_VACCINE_LABELS).find((key) => code.startsWith(key.toLowerCase())) ?? ""
  ];
  if (!vaccine) return "";
  const path = code.includes("_kid") ? "Kid course" : code.includes("_adult") ? "Adult course" : "";
  const timing = code.match(/_(?:(\d+)(w|m|yr)|(w|m|yr)(\d+))$/);
  const timingValue = timing?.[1] ?? timing?.[4] ?? "";
  const timingUnit = timing?.[2] ?? timing?.[3] ?? "";
  const timingLabel = timing
    ? `${timingValue} ${
        timingUnit === "w"
          ? timingValue === "1"
            ? "week"
            : "weeks"
          : timingUnit === "m"
            ? timingValue === "1"
              ? "month"
              : "months"
            : timingValue === "1"
              ? "year"
              : "years"
      }`
    : "";
  return [vaccine, path, timingLabel].filter(Boolean).join(" ");
}

function naturalMatrixScheduleLabel(raw: string): string {
  const source = cleanDisplayLabel(raw).replace(MATRIX_PREFIX_RE, "").trim();
  const match = source.match(/\b(ET\+TT|ETTT|ET_TT|FMD|HS|PPR|Blue Tongue|Goat Pox|Sheep Pox)\b/i);
  if (!match) return "";
  const key = match[1].toUpperCase().replace(/[^A-Z0-9]+/g, "_").replace(/^_+|_+$/g, "");
  const vaccine = VACCINE_LABELS[key] ?? MATRIX_VACCINE_LABELS[key] ?? compactDisplayLabel(match[1]);
  const course = source.match(/\b(adult|kid)\s+course\b/i)?.[0]?.toLowerCase() ?? "";
  const dose = source.match(/\bdose\s+\d+\b/i)?.[0]?.toLowerCase() ?? "";
  return [vaccine, course, dose].filter(Boolean).join(" ");
}

export function vaccinationProtocolDisplayName(protocol: NamedProtocol): string {
  return compactDisplayLabel(protocol.name) || normalizedProtocolName(protocol) || "Vaccination protocol";
}

// Clean a raw drive/protocol display string (e.g. "FMD (TEST-ONLY local development) - seed_fmd-PRIMARY")
// down to the canonical vaccine label + dose phase ("FMD · Primary"). Strips the TEST-ONLY/seed-code noise so
// the execution board doesn't render verbose fixture identifiers, consistent with the matrix column labels.
export function vaccinationDriveDisplayName(raw: string | undefined | null): string {
  const source = (raw ?? "").trim();
  if (!source) return "Vaccination drive";
  const naturalMatrixLabel = naturalMatrixScheduleLabel(source);
  if (naturalMatrixLabel) return naturalMatrixLabel;
  const matrixLabel = matrixScheduleLabel(source);
  if (matrixLabel) return matrixLabel;
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

export function sortVaccinationProtocols<T extends NamedProtocol>(protocols: T[]): T[] {
  return [...protocols].sort((a, b) => vaccinationProtocolDisplayName(a).localeCompare(vaccinationProtocolDisplayName(b)));
}
