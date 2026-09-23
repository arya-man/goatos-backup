#!/usr/bin/env node
// verify-agents-split.mjs — proves the AGENTS.md slim-down dropped no rule.
// Every non-blank line of the pre-split AGENTS.md (default: `git show origin/main:AGENTS.md`,
// override with --base <git-ref> or --file <path>) must appear verbatim as a line in
// AGENTS.md or docs/agent-rules/*.md. Leading '#' heading markers are ignored so moved
// headings may change level; nothing else is normalized.
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const args = process.argv.slice(2);
const flag = (name) => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : undefined; };

const original = flag("--file")
  ? readFileSync(resolve(flag("--file")), "utf8")
  : execFileSync("git", ["show", `${flag("--base") ?? "origin/main"}:AGENTS.md`], { cwd: root, encoding: "utf8", maxBuffer: 64 << 20 });

const norm = (l) => l.replace(/^#{1,6} /, "");
const targets = ["AGENTS.md", ...readdirSync(resolve(root, "docs/agent-rules")).filter((f) => f.endsWith(".md")).sort().map((f) => `docs/agent-rules/${f}`)];
// Multiset: a line that occurs N times in the original must occur >= N times in the split,
// so a dropped duplicate (e.g. a repeated bullet) is still caught.
const have = new Map();
for (const rel of targets) for (const l of readFileSync(resolve(root, rel), "utf8").split("\n")) have.set(norm(l), (have.get(norm(l)) ?? 0) + 1);

const missing = [];
original.split("\n").forEach((l, i) => {
  if (l.trim() === "") return;
  const n = have.get(norm(l)) ?? 0;
  if (n > 0) have.set(norm(l), n - 1);
  else missing.push(`  line ${i + 1}: ${l}`);
});

const origLines = original.split("\n").filter((l) => l.trim() !== "").length;
console.log(`verify-agents-split: checked ${origLines} non-blank original lines against ${targets.length} files`);
if (missing.length) {
  console.error(`verify-agents-split: FAIL — ${missing.length} missing line(s):\n${missing.join("\n")}`);
  process.exit(1);
}
console.log("verify-agents-split: PASS — 0 missing lines");
