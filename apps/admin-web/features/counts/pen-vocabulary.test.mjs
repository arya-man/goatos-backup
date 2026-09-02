import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const ADMIN_WEB = new URL("../../", import.meta.url).pathname;
const SHED = /\b[Ss]heds?\b/;
const TEXT_LINE = /^[A-Za-z0-9][A-Za-z0-9 /·.,;:'()&!?—-]*$/;

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
    let textRun = null;
    const flushTextRun = () => {
      if (!textRun) return;
      const text = textRun.parts.join(" ").replace(/\s+/g, " ").trim();
      if (text && SHED.test(text)) offences.push(`${file}:${textRun.line}  ${text}`);
      textRun = null;
    };
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

      // Text nodes often wrap across physical source lines. Only enter this mode after a JSX
      // opening tag that occupies the line, so TypeScript expressions containing ">" stay out.
      if (/^<[\w.][^>]*>$/.test(trimmed) && !trimmed.endsWith("/>")) {
        flushTextRun();
        textRun = { line: i + 1, parts: [] };
        return;
      }
      if (textRun && TEXT_LINE.test(trimmed)) {
        if (textRun.parts.length === 0) textRun.line = i + 1;
        textRun.parts.push(trimmed);
        return;
      }
      flushTextRun();
    });
    flushTextRun();
  }
  assert.deepEqual(offences, [], `admin-web JSX text still says "shed":\n  ${offences.join("\n  ")}`);
});
