#!/usr/bin/env node
import { execFileSync } from "node:child_process";

const blocked = [
  { value: "SGV2YQ==", caseSensitive: false },
  { value: "U2xpY2U=", caseSensitive: true },
  { value: "aGV2YXBsYXRmb3Jt", caseSensitive: false },
].map((entry) => ({
  term: Buffer.from(entry.value, "base64").toString("utf8"),
  caseSensitive: entry.caseSensitive,
}));
const goSortPrefixPattern = new RegExp("^\\s*sort\\.Sli" + "ce(?:Stable)?\\(");
const pluralSortWordPattern = new RegExp("^(\\s*// .*|\\s*func Test.*)Sli" + "ces[A-Z]");

function git(args) {
  return execFileSync("git", args, { encoding: "utf8" }).trim();
}

function diffFor(args) {
  try {
    return execFileSync("git", ["diff", "--unified=0", "--no-ext-diff", ...args], {
      encoding: "utf8",
      maxBuffer: 20 * 1024 * 1024,
    });
  } catch (error) {
    const out = error.stdout?.toString() ?? "";
    const err = error.stderr?.toString() ?? "";
    throw new Error((out + err).trim() || error.message);
  }
}

// Lines removed anywhere in the same diff. An added line identical to a removed
// one is a pure move (e.g. splitting a doc) and introduces no new mention.
function removedLines(diffText) {
  const removed = new Set();
  for (const line of diffText.split(/\r?\n/)) {
    if (line.startsWith("-") && !line.startsWith("---")) removed.add(line.slice(1).trim());
  }
  return removed;
}

function addedLineFindings(diffText, moved = removedLines(diffText)) {
  const findings = [];
  let file = "";
  for (const line of diffText.split(/\r?\n/)) {
    if (line.startsWith("+++ b/")) {
      file = line.slice("+++ b/".length);
      continue;
    }
    if (!line.startsWith("+") || line.startsWith("+++")) continue;
    const added = line.slice(1);
    if (moved.has(added.trim())) continue;
    for (const { term, caseSensitive } of blocked) {
      const haystack = caseSensitive ? added : added.toLowerCase();
      const needle = caseSensitive ? term : term.toLowerCase();
      if (haystack.includes(needle)) {
        if (term === blocked[1].term && (goSortPrefixPattern.test(added) || pluralSortWordPattern.test(added))) {
          continue;
        }
        findings.push({ file, term, line: added });
      }
    }
  }
  return findings;
}

function collectFindings() {
  const baseRef = process.env.GOATOS_ORG_BOUNDARY_BASE || "origin/main";
  let base = "";
  try {
    base = git(["merge-base", "HEAD", baseRef]);
  } catch {
    base = git(["rev-parse", "HEAD~1"]);
  }

  const diffs = [
    diffFor([`${base}...HEAD`]),
    diffFor(["--cached"]),
    diffFor([]),
  ];
  const moved = new Set(diffs.flatMap((d) => [...removedLines(d)]));
  return diffs.flatMap((d) => addedLineFindings(d, moved));
}

function selfTest() {
  const [a, b, c] = blocked.map((entry) => entry.term);
  const sample = [
    "diff --git a/x b/x",
    "+++ b/x",
    `+${a} reference`,
    `+lower ${b.toLowerCase()} reference`,
    `+${c} reference`,
    "+ordinary line.slice(1) API use",
    "+Mesha reference",
  ].join("\n");
  const findings = addedLineFindings(sample);
  if (findings.length !== 3) {
    throw new Error(`expected 3 findings, got ${findings.length}`);
  }
  // A verbatim move is not a new mention; an edited "move" still is.
  const moveSample = ["+++ b/y", `-keep ${a} rule`, `+keep ${a} rule`, `+keep ${a} rule, reworded`].join("\n");
  const moveFindings = addedLineFindings(moveSample);
  if (moveFindings.length !== 1) {
    throw new Error(`expected 1 finding for moved+edited lines, got ${moveFindings.length}`);
  }
  console.log("org-boundary self-test: PASS");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = collectFindings();
if (findings.length > 0) {
  console.error("org-boundary guard: blocked new cross-organization/project name mention(s).");
  for (const finding of findings) {
    console.error(`- ${finding.file}: ${finding.term}: ${finding.line}`);
  }
  process.exit(1);
}

console.log("org-boundary guard: PASS");
