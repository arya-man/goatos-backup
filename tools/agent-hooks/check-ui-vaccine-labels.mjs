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

// One group per UI surface, with a floor. `git ls-files` pathspecs do NOT brace-
// expand, so the single pattern "apps/admin-web/**/*.{ts,tsx,js,jsx}" that used
// to live here matched ZERO files: every admin-web page was unscanned while this
// guard printed PASS. Measured 2026-09-23 — a raw `et_tt_adult_w2` in a .tsx
// passed, the identical literal in a .kt failed. The `min` floor is what makes
// that class of silence impossible: a surface that matches nothing is a FAILURE,
// not a pass (CONTRACT.md §4 — a check that did not run renders no verdict).
const UI_GLOB_GROUPS = [
  {
    surface: "admin-web",
    globs: [
      "apps/admin-web/**/*.ts",
      "apps/admin-web/**/*.tsx",
      "apps/admin-web/**/*.js",
      "apps/admin-web/**/*.jsx",
    ],
    min: 50,
  },
  { surface: "android", globs: ["apps/goatos-android/**/*.kt"], min: 50 },
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
  // A comment cannot render. `/**` and `/*` were missing here, which is why a
  // JSDoc line like `/** e.g. "ET_TT". */` counted as user-facing copy when the
  // admin-web surface was first actually scanned (2026-09-23).
  if (!trimmed || trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*")) return false;
  if (ALLOWED_LINE_MARKERS.some((marker) => line.includes(marker))) return false;

  // The risky cases are string/template literals in render/state/copy code. We
  // intentionally do not flag bare identifiers, imports, generated schemas, or
  // config maps.
  return /["'`][^"'`]*(?:preventive\s+care\s+vaccination\s+matrix|et_tt|blue_tongue|goat_pox|sheep_pox|adult_w\d+|kid_w\d+|_adult_w\d+|_kid_w\d+|_booster|_first)[^"'`]*["'`]/i.test(line);
}

function isRawUiFallbackLine(line) {
  return /\b(?:title|label|detail|subtitle|description|text)\s*:\s*(?:[^,\n]*\|\|\s*)?(?:raw|withoutPrefix)\b/.test(line);
}

function allowedMapperLines(source) {
  const allowed = new Set();
  const lines = source.split(/\r?\n/);
  let inMapper = false;
  let braceDepth = 0;
  let inTsMapper = false;
  let bracketDepth = 0;
  lines.forEach((line, index) => {
    if (/fun\s+(?:calendarCategoryLabel|humanizeVaccineLabel)\s*\(/.test(line) || /function\s+displayVaccine/.test(line)) {
      inMapper = true;
      braceDepth = 0;
    }
    if (inMapper) {
      allowed.add(index + 1);
      braceDepth += (line.match(/\{/g) ?? []).length;
      braceDepth -= (line.match(/\}/g) ?? []).length;
      if (braceDepth <= 0 && line.includes("}")) inMapper = false;
    }
    // A TS/JS code -> copy-key table IS the display mapper this guard tells you
    // to write; the raw token has to appear on its left-hand side. Recognised by
    // the declaration name, and closed by its own bracket depth, so it exempts
    // the table and nothing after it.
    if (/(?:const|let|var)\s+VACCINE_[A-Z0-9_]*(?:COPY|LABEL|DISPLAY|NAME)[A-Z0-9_]*\s*[:=]/.test(line)) {
      inTsMapper = true;
      bracketDepth = 0;
    }
    if (inTsMapper) {
      allowed.add(index + 1);
      bracketDepth += (line.match(/[[{]/g) ?? []).length;
      bracketDepth -= (line.match(/[\]}]/g) ?? []).length;
      if (bracketDepth <= 0 && /[\]}]/.test(line)) inTsMapper = false;
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
      if (
        ((TOKEN_RE.test(line) && isProbablyUserFacingLine(line)) || isRawUiFallbackLine(line)) &&
        !mapperLines.has(lineNumber)
      ) {
        findings.push(`${rel}:${lineNumber}: raw vaccine token may render in UI; map it to a human label first`);
      }
    });
  }
  return findings;
}

// Returns { files, counts } so the caller can prove each surface was actually
// looked at instead of assuming it was.
function listFiles() {
  const counts = {};
  const files = [];
  for (const group of UI_GLOB_GROUPS) {
    const output = execFileSync("git", ["ls-files", ...group.globs], {
      cwd: repo,
      encoding: "utf8",
    });
    const rows = output.split(/\r?\n/).filter(Boolean);
    counts[group.surface] = rows.length;
    for (const rel of rows) files.push(resolve(repo, rel));
  }
  return { files, counts };
}

function emptySurfaces(counts) {
  return UI_GLOB_GROUPS.filter((g) => (counts[g.surface] ?? 0) < g.min).map((g) => ({
    surface: g.surface,
    saw: counts[g.surface] ?? 0,
    min: g.min,
  }));
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
    const badFallback = join(dir, "apps/admin-web/features/vaccination/z.tsx");
    execFileSync("mkdir", ["-p", dirname(badFallback)]);
    writeFileSync(badFallback, "return { detail: withoutPrefix || raw };\n");
    const findings = findingsForFiles([bad, badWithConfigWord, goodConfig, goodMapper, badFallback], dir);
    if (
      findings.length !== 3 ||
      !findings.some((finding) => finding.includes("X.kt:1")) ||
      !findings.some((finding) => finding.includes("y.tsx:1")) ||
      !findings.some((finding) => finding.includes("z.tsx:1"))
    ) {
      throw new Error(`self-test expected exactly three UI leak findings, got:\n${findings.join("\n")}`);
    }
    // The part the old self-test never touched: the real scan. The brace-glob
    // bug lived here for as long as it did because every fixture was passed to
    // findingsForFiles() by hand.
    const { counts } = listFiles();
    const empty = emptySurfaces(counts);
    if (empty.length) {
      throw new Error(
        `self-test: the real scan covers no files for: ${empty
          .map((e) => `${e.surface} (${e.saw} < ${e.min})`)
          .join(", ")}`,
      );
    }
    // And the planted violation, against the real scan's own file list: a raw
    // token in an admin-web .tsx must be a finding, not silence.
    const planted = join(repo, "apps/admin-web/features/vaccination/__guard_probe__.tsx");
    execFileSync("mkdir", ["-p", dirname(planted)]);
    writeFileSync(planted, 'export const label = "et_tt_adult_w2";\n');
    try {
      const plantedFindings = findingsForFiles([planted]);
      if (plantedFindings.length !== 1) {
        throw new Error(`self-test: a raw vaccine token in an admin-web .tsx was not caught: ${plantedFindings.join(", ")}`);
      }
    } finally {
      rmSync(planted, { force: true });
    }
    console.log(
      `check-ui-vaccine-labels self-test: PASS (real scan: ${Object.entries(counts)
        .map(([k, v]) => `${k}=${v}`)
        .join(", ")})`,
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const { files, counts } = listFiles();
  const empty = emptySurfaces(counts);
  if (empty.length) {
    console.error("check-ui-vaccine-labels: FAIL — a UI surface matched (almost) no files, so it was not checked:");
    for (const e of empty) console.error(`- ${e.surface}: ${e.saw} file(s), expected at least ${e.min}`);
    console.error("\nFix the pathspec in UI_GLOB_GROUPS. A surface that matches nothing is not a pass.");
    process.exit(1);
  }
  const findings = findingsForFiles(files);
  if (findings.length) {
    console.error("Raw vaccine config tokens must not render in UI:");
    for (const finding of findings) console.error(`- ${finding}`);
    console.error("\nUse a display mapper/copy contract. Raw codes are allowed only in config/contracts/DTOs/tests.");
    process.exit(1);
  }
  console.log("check-ui-vaccine-labels: PASS");
}
