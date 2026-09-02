import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const ADMIN_WEB = new URL("../../", import.meta.url).pathname;
const SHED = /\b[Ss]heds?\b/;

function walk(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry === ".next" || entry.startsWith(".")) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (full.endsWith(".tsx")) out.push(full);
  }
  return out;
}

// The dashboard says PEN, never SHED (docs/decisions/pen-not-shed-vocabulary.md).
//
// This guard exists because the first sweep of this rename missed a whole CLASS of copy: it
// replaced quoted string literals, and JSX TEXT NODES are not quoted. `<option>All sheds</option>`
// and a bare `Shed` label line both survived it and rendered on Live Monitor. Attribute copy is
// quoted and is caught by the backend contract test; this covers the half that is not.
//
// It scans TEXT the browser paints, not identifiers: `shed_id`, `shedId`, `data-l` keys and
// `/vaccination/sheds` do not match, because the word boundary excludes them by construction.
test("no admin-web JSX text node says shed", () => {
  const offences = [];
  for (const file of walk(ADMIN_WEB)) {
    const lines = readFileSync(file, "utf8").split("\n");
    let inBlockComment = false;
    lines.forEach((line, i) => {
      const trimmed = line.trim();
      // Skip comments: `//`, `/* */`, and JSX `{/* */}` blocks, which carry prose about the
      // storage model and legitimately name both words.
      if (inBlockComment) {
        if (trimmed.includes("*/")) inBlockComment = false;
        return;
      }
      if (trimmed.startsWith("//") || trimmed.startsWith("*")) return;
      if (trimmed.includes("/*") && !trimmed.includes("*/")) { inBlockComment = true; return; }
      if (trimmed.startsWith("{/*")) return;

      // Text between tags, with no braces or quotes in it -- i.e. a literal the user reads.
      for (const match of line.matchAll(/>([^<>{}"']*?)</g)) {
        const text = match[1].trim();
        if (text && SHED.test(text)) offences.push(`${file}:${i + 1}  ${text}`);
      }
      // A label on its own line between tags, e.g. `<span className="fsel">\n  Shed\n  <select`
      if (/^[A-Za-z][A-Za-z /·—-]*$/.test(trimmed) && SHED.test(trimmed)) {
        offences.push(`${file}:${i + 1}  ${trimmed}`);
      }
    });
  }
  assert.deepEqual(offences, [], `admin-web JSX text still says "shed":\n  ${offences.join("\n  ")}`);
});
