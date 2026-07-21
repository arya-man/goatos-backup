#!/usr/bin/env node

// check-ui-vaccine-labels.mjs
//
// Hard product/UI guardrail:
// vaccine protocol/config tokens such as `et_tt_adult_w2`, `ppr_booster`,
// `blue_tongue_first`, or the protocol family name "Preventive Care
// Vaccination Matrix" are allowed as backend/config identifiers, but they must
// never be rendered directly as user-facing copy in admin-web or Android UI.
// UI code must pass API/config values through a display mapper and show human
// labels ("ET+TT", "PPR · Booster", "Blue Tongue", ...).

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

const TOKEN_RE = /(?:preventive\s+care\s+vaccination\s+matrix|(?:^|[^a-z0-9])(?:et_tt|blue_tongue|goat_pox|sheep_pox|adult_w\d+|kid_w\d+|_adult_w\d+|_kid_w\d+|_booster|_first)(?:$|[^a-z0-9]))/i;

const UI_GLOBS = [
  "apps/admin-web/**/*.{ts,tsx,js,jsx}",
  "apps/goatos-android/**/*.kt",
];

const ALLOWED_PATH_PARTS = [
  "/features/config/",
  "/lib/admin-ui-contract.ts",
  "/features/preventive-care-vaccination/vaccine-display.ts",
  "/core/core-network/",
  "/core/core-data/schemas/",
  "/src/androidTest/",
  "/src/debug/",
  "/build/",
];

const SCREENSHOT_UI_TEST_PATH_PARTS = [
  "/apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/ui/",
];

const ALLOWED_LINE_MARKERS = [
  "humanizeVaccineLabel(",
  "displayVaccine",
  "vaccineDisplay",
  "VACCINE_LABEL",
  "Raw inputs",
  "Raw input",
  "vaccine_code",
  "vaccineCode",
  "dose_code",
  "doseCode",
];

function isAllowedPath(path) {
  const normalized = `/${path.replaceAll("\\", "/")}`;
  if (SCREENSHOT_UI_TEST_PATH_PARTS.some((part) => normalized.includes(part))) return false;
  return ALLOWED_PATH_PARTS.some((part) => normalized.includes(part));
}

function isProbablyUserFacingLine(line) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("//") || trimmed.startsWith("*")) return false;
  if (ALLOWED_LINE_MARKERS.some((marker) => line.includes(marker))) return false;

  // The risky cases are string/template literals in render/state/copy code. We
  // intentionally do not flag bare identifiers, imports, generated schemas, or
  // config maps.
  return /["'`][^"'`]*(?:preventive\s+care\s+vaccination\s+matrix|et_tt|blue_tongue|goat_pox|sheep_pox|adult_w\d+|kid_w\d+|_adult_w\d+|_kid_w\d+|_booster|_first)[^"'`]*["'`]/i.test(line);
}

function allowedMapperLines(source) {
  const allowed = new Set();
  const lines = source.split(/\r?\n/);
  let inMapper = false;
  let braceDepth = 0;
  lines.forEach((line, index) => {
    if (/fun\s+humanizeVaccineLabel\s*\(/.test(line) || /function\s+displayVaccine/.test(line)) {
      inMapper = true;
      braceDepth = 0;
    }
    if (inMapper) {
      allowed.add(index + 1);
      braceDepth += (line.match(/\{/g) ?? []).length;
      braceDepth -= (line.match(/\}/g) ?? []).length;
      if (braceDepth <= 0 && line.includes("}")) inMapper = false;
    }
  });
  return allowed;
}

export function findingsForFiles(files, root = repo) {
  const findings = [];
  for (const file of files) {
    const rel = relative(root, file);
    if (isAllowedPath(rel)) continue;
    const source = readFileSync(file, "utf8");
    const mapperLines = allowedMapperLines(source);
    source.split(/\r?\n/).forEach((line, index) => {
      const lineNumber = index + 1;
      if (TOKEN_RE.test(line) && isProbablyUserFacingLine(line) && !mapperLines.has(lineNumber)) {
        findings.push(`${rel}:${lineNumber}: raw vaccine token may render in UI; map it to a human label first`);
      }
    });
  }
  return findings;
}

function listFiles() {
  const output = execFileSync("git", ["ls-files", ...UI_GLOBS], {
    cwd: repo,
    encoding: "utf8",
  });
  return output
    .split(/\r?\n/)
    .filter(Boolean)
    .map((rel) => resolve(repo, rel));
}

function selfTest() {
  const dir = mkdtempSync(join(tmpdir(), "goatos-ui-vaccine-labels-"));
  try {
    const bad = join(dir, "apps/goatos-android/feature/feature-sheds/src/main/kotlin/X.kt");
    const badWithConfigWord = join(dir, "apps/admin-web/features/vaccination/y.tsx");
    const goodConfig = join(dir, "apps/admin-web/features/config/x.tsx");
    const goodMapper = join(dir, "apps/admin-web/features/preventive-care-vaccination/vaccine-display.ts");
    execFileSync("mkdir", ["-p", dirname(bad), dirname(badWithConfigWord), dirname(goodConfig), dirname(goodMapper)]);
    writeFileSync(bad, 'Text("Preventive Care Vaccination Matrix - et_tt_adult_w2")\n');
    writeFileSync(badWithConfigWord, 'export const title = "Config says et_tt_adult_w2";\n');
    writeFileSync(goodConfig, 'export const code = "et_tt_adult_w2";\n');
    writeFileSync(goodMapper, 'export const VACCINE_LABELS = { et_tt: "ET+TT" };\n');
    const findings = findingsForFiles([bad, badWithConfigWord, goodConfig, goodMapper], dir);
    if (
      findings.length !== 2 ||
      !findings.some((finding) => finding.includes("X.kt:1")) ||
      !findings.some((finding) => finding.includes("y.tsx:1"))
    ) {
      throw new Error(`self-test expected exactly two UI leak findings, got:\n${findings.join("\n")}`);
    }
    console.log("check-ui-vaccine-labels self-test: PASS");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const findings = findingsForFiles(listFiles());
  if (findings.length) {
    console.error("Raw vaccine config tokens must not render in UI:");
    for (const finding of findings) console.error(`- ${finding}`);
    console.error("\nUse a display mapper/copy contract. Raw codes are allowed only in config/contracts/DTOs/tests.");
    process.exit(1);
  }
  console.log("check-ui-vaccine-labels: PASS");
}
