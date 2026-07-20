#!/usr/bin/env node
// App-wide Android UI copy/layout guard.
//
// Scans every production Compose surface (app, feature modules, and core-ui), not only
// vaccination. It blocks:
//   - internal API/storage vocabulary rendered as user-facing copy;
//   - raw UUID literals rendered as copy;
//   - manual translation/absolute-offset alignment hacks that produce fragile layouts.
//   - one-letter/narrow date labels that collapse calendar strips on real data.
//   - state-dependent geometry that makes peer cards/buttons/chips render with mixed sizes.
//   - translatable base resources missing from any supported app locale.
//
// The checks intentionally target display sinks and Android string resources rather than every
// source literal, so wire DTOs, comments, tests, and internal identifiers remain legal.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const root = process.cwd();
const scanRoots = [
  "apps/goatos-android/app/src/main",
  "apps/goatos-android/feature",
  "apps/goatos-android/core/core-ui/src/main",
];
const supportedLocales = ["hi", "kn", "te"];

const internalToken =
  /\b(?:task_id|row_version|proof_id|submission_id|subject_id|scope_id|trace_id|idempotency_key|provider_message_id)\b/i;
const uuidLiteral = /\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b/i;
const stateToken = String.raw`\b(?:selected|isSelected|active|checked|current|status|tone|done|enabled|loading)\b`;
const geometryCall = String.raw`(?:Modifier\.)?(?:padding|height|width|size|heightIn|widthIn)\s*\(`;
const geometryName = String.raw`\b[a-zA-Z0-9_]*(?:height|width|size|padding|spacing|inset|offset)[a-zA-Z0-9_]*\b`;

function walk(dir, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "test", "androidTest", "debug"].includes(entry.name)) continue;
      walk(full, out);
    } else if (entry.name.endsWith(".kt") || entry.name === "strings.xml") {
      out.push(full);
    }
  }
  return out;
}

function walkBaseStringFiles(dir, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "build") walkBaseStringFiles(full, out);
    } else if (
      entry.name === "strings.xml" &&
      full.endsWith(`${path.sep}src${path.sep}main${path.sep}res${path.sep}values${path.sep}strings.xml`)
    ) {
      out.push(full);
    }
  }
  return out;
}

function lineNumber(text, index) {
  return text.slice(0, index).split("\n").length;
}

function translatableResourceEntries(xml) {
  const entries = new Map();
  const resourceTag = /<(string-array|plurals|string)\b([^>]*\bname\s*=\s*"([^"]+)"[^>]*)>/g;
  for (const match of xml.matchAll(resourceTag)) {
    if (/\btranslatable\s*=\s*"false"/.test(match[2])) continue;
    entries.set(match[3], { index: match.index ?? 0, type: match[1] });
  }
  return entries;
}

function localeParityFindings(baseFile, read = (file) => fs.readFileSync(file, "utf8"), exists = fs.existsSync) {
  const findings = [];
  const baseXml = read(baseFile);
  const baseEntries = translatableResourceEntries(baseXml);
  // Identifier-only modules do not need empty translated resource files.
  if (baseEntries.size === 0) return findings;

  for (const locale of supportedLocales) {
    const localizedFile = baseFile.replace(
      `${path.sep}values${path.sep}strings.xml`,
      `${path.sep}values-${locale}${path.sep}strings.xml`,
    );
    const localizedEntries = exists(localizedFile)
      ? translatableResourceEntries(read(localizedFile))
      : new Map();
    for (const [name, entry] of baseEntries) {
      if (!localizedEntries.has(name)) {
        findings.push({
          rel: path.relative(root, baseFile),
          line: lineNumber(baseXml, entry.index),
          message: `translatable resource '${name}' is missing from values-${locale}/strings.xml`,
        });
      }
    }
  }
  return findings;
}

function findingsFor(file, text) {
  const findings = [];
  const rel = path.relative(root, file);
  const addMatches = (regex, message) => {
    for (const match of text.matchAll(regex)) {
      findings.push({ rel, line: lineNumber(text, match.index ?? 0), message });
    }
  };

  if (file.endsWith("strings.xml")) {
    for (const match of text.matchAll(/<string\b[^>]*>([\s\S]*?)<\/string>/g)) {
      const value = match[1].replace(/<[^>]+>/g, " ");
      if (internalToken.test(value)) {
        findings.push({
          rel,
          line: lineNumber(text, match.index ?? 0),
          message: "user-facing string exposes an internal API/storage field name",
        });
      }
      if (uuidLiteral.test(value)) {
        findings.push({
          rel,
          line: lineNumber(text, match.index ?? 0),
          message: "user-facing string exposes a raw UUID",
        });
      }
    }
    return findings;
  }

  // Only inspect literal copy passed to common visible/accessibility sinks.
  for (const match of text.matchAll(
    /(?:Text|AnnotatedString)\s*\(\s*"([^"]*)"|(?:title|subtitle|label|placeholder|contentDescription)\s*=\s*"([^"]*)"/g,
  )) {
    const value = match[1] ?? match[2] ?? "";
    if (internalToken.test(value)) {
      findings.push({
        rel,
        line: lineNumber(text, match.index ?? 0),
        message: "visible copy exposes an internal API/storage field name",
      });
    }
    if (uuidLiteral.test(value)) {
      findings.push({
        rel,
        line: lineNumber(text, match.index ?? 0),
        message: "visible copy exposes a raw UUID",
      });
    }
  }

  addMatches(/\babsoluteOffset\s*\(/g, "absoluteOffset is a fragile manual-alignment hack");
  addMatches(/\btranslation[XY]\s*=/g, "graphics translation is a fragile manual-alignment hack");
  addMatches(/\b(?:Modifier\.)?offset\s*\([^)]*-\s*\d+(?:\.\d+)?\.dp/g, "negative offset is a fragile manual-alignment hack");
  addMatches(
    new RegExp(String.raw`\bif\s*\([^)\n]*${stateToken}[^)\n]*\)[^\n]*(?:Modifier\.)?(?:padding|height|width|size|heightIn|widthIn)\s*\(`, "gi"),
    "state must not change peer component geometry; keep cards/buttons/chips fixed-size and change color/copy only",
  );
  addMatches(
    new RegExp(String.raw`${geometryCall}\s*\bif\s*\([^)\n]*${stateToken}`, "gi"),
    "state must not change peer component geometry inside padding/height/width/size calls",
  );
  addMatches(
    new RegExp(String.raw`\b(?:val|var)\s+${geometryName}\s*=\s*if\s*\([^)]*${stateToken}[^)]*\)[\s\S]{0,120}?(?:\.dp\b|MeshaDimens\.|Dp\b)`, "gi"),
    "state must not compute alternate peer geometry; keep sibling cards/buttons/chips fixed-size",
  );
  addMatches(
    new RegExp(String.raw`\b(?:val|var)\s+${geometryName}\s*=\s*when\s*\([^)]*${stateToken}[^)]*\)[\s\S]{0,180}?(?:\.dp\b|MeshaDimens\.|Dp\b)`, "gi"),
    "state must not compute alternate peer geometry; keep sibling cards/buttons/chips fixed-size",
  );
  addMatches(
    new RegExp(String.raw`${geometryCall}\s*\n[\s\S]{0,80}?\bif\s*\([^)]*${stateToken}[^)]*\)`, "gi"),
    "state must not change peer component geometry inside multiline modifier arguments",
  );
  addMatches(
    /\bTextStyle\.NARROW\b/g,
    "TextStyle.NARROW creates one-letter date labels; use readable short labels for mobile UI",
  );
  addMatches(
    /CalendarWeekDay\s*\(\s*[^,\n]+,\s*"[MTWFS]"/g,
    "CalendarWeekDay fixtures must use readable short weekday labels, not one-letter date chips",
  );
  if (rel.endsWith("feature-calendar/src/main/kotlin/sg/mesha/goatos/feature/calendar/CalendarScreen.kt")) {
    if (!/val\s+dayCellHeight\s*=/.test(text) || !/WeekDayCell\([\s\S]*?modifier\s*=\s*Modifier\.weight\(1f\)\.height\(dayCellHeight\)/.test(text)) {
      findings.push({
        rel,
        line: 1,
        message: "calendar week strip must give every day chip the same fixed height",
      });
    }
  }
  if (rel.endsWith("app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt")) {
    addMatches(
      /\b(?:shed|cohort|date|subtitle|eyebrow|title)\s*=\s*task\.(?:title|description|scopeId|taskId|sopCode)\b/g,
      "task transport fields/internal ids must not feed visible submit copy; render TaskPresentation",
    );
  }
  if (rel.endsWith("backend/internal/obligation/app/sweeper.go")) {
    addMatches(
      /\bTitle\s*:\s*[^\n]*(?:ScopeID|BatchID|TaskID)/g,
      "task creation must not concatenate internal ids into a user-visible title",
    );
  }
  return findings;
}

function runSelfTest() {
  const bad = `
    Text("proof_id")
    Modifier.absoluteOffset(x = 12.dp)
    Modifier.graphicsLayer { translationX = 8f }
    val label = TextStyle.NARROW
    CalendarWeekDay("d7", "T", "7", "")
    if (selected) Modifier.height(72.dp) else Modifier.height(60.dp)
    Modifier.padding(if (active) 12.dp else 8.dp)
    val selectedHeight = if (selected) 72.dp else 60.dp
    val buttonPadding = when (status) { Good -> 12.dp else -> 8.dp }
  `;
  const good = `
    Text(stringResource(R.string.proof_ready))
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp))
  `;
  const badTaskPresentation = `shed = task.title\nTitle: "Vaccination drive " + b.ScopeID`;
  const badFindings = findingsFor("Screen.kt", bad);
  const goodFindings = findingsFor("Screen.kt", good);
  const badVmFindings = findingsFor("app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt", badTaskPresentation);
  const badBackendFindings = findingsFor("backend/internal/obligation/app/sweeper.go", badTaskPresentation);
  const fixtureBase = path.join(root, "fixture", "src", "main", "res", "values", "strings.xml");
  const fixtureFiles = new Map([
    [fixtureBase, `<resources><string name="visible">Visible</string><string name="channel_id" translatable="false">id</string></resources>`],
    [fixtureBase.replace(`${path.sep}values${path.sep}`, `${path.sep}values-hi${path.sep}`), `<resources><string name="visible">दिखने वाला</string></resources>`],
    [fixtureBase.replace(`${path.sep}values${path.sep}`, `${path.sep}values-te${path.sep}`), `<resources><string name="visible">కనిపించేది</string></resources>`],
  ]);
  const localeFindings = localeParityFindings(
    fixtureBase,
    (file) => fixtureFiles.get(file),
    (file) => fixtureFiles.has(file),
  );
  const identifierOnlyBase = fixtureBase.replace("fixture", "identifier-only");
  const identifierOnlyFindings = localeParityFindings(
    identifierOnlyBase,
    () => `<resources><string name="channel_id" translatable="false">id</string></resources>`,
    () => false,
  );
  if (badFindings.length !== 9 || goodFindings.length !== 0 || badVmFindings.length !== 1 || badBackendFindings.length !== 1) {
    throw new Error(
      `self-test failed: bad=${JSON.stringify(badFindings)} good=${JSON.stringify(goodFindings)} vm=${JSON.stringify(badVmFindings)} backend=${JSON.stringify(badBackendFindings)}`,
    );
  }
  if (localeFindings.length !== 1 || !localeFindings[0].message.includes("values-kn") || identifierOnlyFindings.length !== 0) {
    throw new Error(
      `locale self-test failed: missing=${JSON.stringify(localeFindings)} identifierOnly=${JSON.stringify(identifierOnlyFindings)}`,
    );
  }
  console.log("android-ui-copy-layout guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
  process.exit(0);
}

const files = [
  ...scanRoots.flatMap((dir) => walk(path.join(root, dir))),
  path.join(root, "backend/internal/obligation/app/sweeper.go"),
].filter((file) => fs.existsSync(file));
const findings = files.flatMap((file) => findingsFor(file, fs.readFileSync(file, "utf8")));
for (const baseFile of walkBaseStringFiles(path.join(root, "apps/goatos-android"))) {
  findings.push(...localeParityFindings(baseFile));
}
if (findings.length > 0) {
  console.error("android-ui-copy-layout guard FAILED:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line}: ${finding.message}`);
  }
  process.exit(1);
}
console.log(`android-ui-copy-layout guard passed (${files.length} production UI files)`);
