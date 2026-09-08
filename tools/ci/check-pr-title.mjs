#!/usr/bin/env node
// Guard for the Conventional Commits title rule (AGENTS.md -> "PR and Commit
// Title Rule"). It exists because PR #199 shipped with the bare title "Apply the
// Mesha design system across admin-web" while its own commits were correct: the
// rule lived only in the maintainer's head and in `git log`, so an agent that
// read neither had nothing to violate.
//
//   node tools/ci/check-pr-title.mjs                 # the open PR for this branch (needs gh)
//   node tools/ci/check-pr-title.mjs "feat(x): y"    # an explicit title
//   node tools/ci/check-pr-title.mjs --self-test
import { execFileSync } from "node:child_process";

const TYPES = ["feat", "fix", "perf", "refactor", "docs", "test", "chore", "build", "ci"];
// Scopes observed in merged PRs. Not a closed set -- an unknown scope warns
// rather than fails, because a genuinely new product area must be able to land.
const KNOWN_SCOPES = ["feed","sales","verify","vaccination","pc-care","weights","weighing","health",
  "adminui","android","workforce","process-integrity","leadership-tasks","pen-visits","design-system",
  "counts","procurement","calendar","milk","approvals","herd-signals","ceo-ai","infra","deploy"];

const PATTERN = new RegExp(`^(${TYPES.join("|")})\\(([a-z0-9][a-z0-9._/-]*)\\): (\\S.*)$`);

export function checkTitle(title) {
  const problems = [];
  const warnings = [];
  const t = String(title ?? "").trim();
  if (!t) return { ok: false, problems: ["title is empty"], warnings };
  const m = PATTERN.exec(t);
  if (!m) {
    const loose = /^([a-zA-Z]+)(\(([^)]*)\))?\s*:/.exec(t);
    if (!loose) problems.push(`no "type(scope): " prefix -- got ${JSON.stringify(t)}`);
    else if (!TYPES.includes(loose[1].toLowerCase())) problems.push(`unknown type ${JSON.stringify(loose[1])}; use one of ${TYPES.join(", ")}`);
    else if (loose[1] !== loose[1].toLowerCase()) problems.push(`type must be lowercase, got ${JSON.stringify(loose[1])}`);
    else if (!loose[2]) problems.push("missing (scope)");
    else problems.push(`malformed scope ${JSON.stringify(loose[3])}; use lowercase kebab-case`);
    return { ok: false, problems, warnings };
  }
  const [, , scope, summary] = m;
  if (!KNOWN_SCOPES.includes(scope)) warnings.push(`scope "${scope}" is new; reuse an existing scope if one fits (${KNOWN_SCOPES.slice(0, 8).join(", ")}, ...)`);
  if (/^[A-Z][a-z]/.test(summary)) problems.push(`summary must be sentence case, not capitalised: ${JSON.stringify(summary)}`);
  if (summary.endsWith(".")) problems.push("summary must not end with a period");
  if (summary.length < 8) problems.push("summary is too short to say what changed");
  return { ok: problems.length === 0, problems, warnings };
}

function selfTest() {
  const good = [
    "feat(sales): vendor average animal weight + weight at tagging",
    "fix(verify): note on approve, and fullscreen resumes where the clip was",
    "perf(process-integrity): bucket the read-cache key so the cache can hit",
    "docs(design-system): add the mesha-design-system skill and index it",
  ];
  const bad = [
    ["Apply the Mesha design system across admin-web", "the real #199 title"],
    ["Fix admin sidebar latency and performance telemetry", "capitalised, no type"],
    ["feat: add a thing to the page", "missing scope"],
    ["Feat(sales): do a thing properly", "capitalised type"],
    ["feat(Sales): do a thing properly", "capitalised scope"],
    ["feat(sales): Do a thing properly", "capitalised summary"],
    ["feat(sales): do a thing properly.", "trailing period"],
    ["feat(sales): fix", "summary too short"],
    ["", "empty"],
  ];
  let failed = 0;
  for (const t of good) if (!checkTitle(t).ok) { console.error(`self-test: expected PASS -> ${t}`); failed++; }
  for (const [t, why] of bad) if (checkTitle(t).ok) { console.error(`self-test: expected FAIL (${why}) -> ${t}`); failed++; }
  // A new-but-well-formed scope warns, it does not fail.
  const novel = checkTitle("feat(brand-new-area): introduce the thing");
  if (!novel.ok || novel.warnings.length !== 1) { console.error("self-test: a novel scope must pass with one warning"); failed++; }
  if (failed) { console.error(`pr-title self-test: FAILED (${failed})`); process.exit(1); }
  console.log(`pr-title self-test: ok (${good.length} valid, ${bad.length} invalid, novel-scope warning)`);
}

const arg = process.argv[2];
if (arg === "--self-test") { selfTest(); process.exit(0); }

let title = arg;
let source = "argument";
if (!title) {
  try {
    title = execFileSync("gh", ["pr", "view", "--json", "title", "-q", ".title"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
    source = "the open PR for this branch";
  } catch {
    console.log("pr-title: no PR for this branch and no title argument; nothing to check.");
    process.exit(0);
  }
}
const { ok, problems, warnings } = checkTitle(title);
for (const w of warnings) console.warn(`pr-title: warning: ${w}`);
if (ok) { console.log(`pr-title: ok (${source}) -- ${title}`); process.exit(0); }
console.error(`\npr-title: ${JSON.stringify(title)} (${source}) does not follow the rule.\n`);
for (const p of problems) console.error(`  - ${p}`);
console.error(`\nUse:  type(scope): imperative summary in sentence case`);
console.error(`Full rule: AGENTS.md -> "PR and Commit Title Rule".`);
process.exit(1);
