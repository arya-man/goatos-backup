#!/usr/bin/env node
// App-wide Android UI copy/layout guard.
//
// Scans every production Compose surface (app, feature modules, and core-ui), not only
// vaccination. It blocks:
//   - internal API/storage vocabulary rendered as user-facing copy;
//   - raw UUID literals rendered as copy;
//   - manual translation/absolute-offset alignment hacks that produce fragile layouts.
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

const internalToken =
  /\b(?:task_id|row_version|proof_id|submission_id|subject_id|scope_id|trace_id|idempotency_key|provider_message_id)\b/i;
const uuidLiteral = /\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b/i;

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

function lineNumber(text, index) {
  return text.slice(0, index).split("\n").length;
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
  return findings;
}

function runSelfTest() {
  const bad = `
    Text("proof_id")
    Modifier.absoluteOffset(x = 12.dp)
    Modifier.graphicsLayer { translationX = 8f }
  `;
  const good = `
    Text(stringResource(R.string.proof_ready))
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp))
  `;
  const badFindings = findingsFor("Screen.kt", bad);
  const goodFindings = findingsFor("Screen.kt", good);
  if (badFindings.length !== 3 || goodFindings.length !== 0) {
    throw new Error(
      `self-test failed: bad=${JSON.stringify(badFindings)} good=${JSON.stringify(goodFindings)}`,
    );
  }
  console.log("android-ui-copy-layout guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
  process.exit(0);
}

const files = scanRoots.flatMap((dir) => walk(path.join(root, dir)));
const findings = files.flatMap((file) => findingsFor(file, fs.readFileSync(file, "utf8")));
if (findings.length > 0) {
  console.error("android-ui-copy-layout guard FAILED:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line}: ${finding.message}`);
  }
  process.exit(1);
}
console.log(`android-ui-copy-layout guard passed (${files.length} production UI files)`);
