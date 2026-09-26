// guard: preflight-pseudo-border — admin-web loads Tailwind preflight, which gives every
// ::before/::after `border-style: solid`. A template pseudo element that sets only borderWidth
// (it relied on the browser default `none`) then draws a solid currentColor ring: the /tasks
// kanban columns had a white outline in dark mode. Such a pseudo block must also name borderStyle.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = join(dirname(fileURLToPath(import.meta.url)), "..", "minimal");

function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(join(dir, e.name)) : e.name.endsWith(".tsx") ? [join(dir, e.name)] : []));
}

export function pseudoBorderOffences(src) {
  const out = [];
  // innermost object literals only
  for (const m of src.matchAll(/\{[^{}]*\}/g)) {
    const block = m[0];
    if (/content:\s*['"`]/.test(block) && /borderWidth\s*:/.test(block) && !/borderStyle\s*:/.test(block) && !/border\s*:/.test(block)) out.push(block.slice(0, 80));
  }
  return out;
}

test("self-test: a pseudo block with borderWidth and no borderStyle is caught", () => {
  assert.equal(pseudoBorderOffences(`idle: { content: '""', borderWidth: '1px', position: 'absolute' }`).length, 1);
  assert.equal(pseudoBorderOffences(`idle: { content: '""', borderWidth: '1px', borderStyle: 'none' }`).length, 0);
});

test("template pseudo elements that set borderWidth also set borderStyle", () => {
  const offenders = [];
  for (const f of files(here)) for (const o of pseudoBorderOffences(readFileSync(f, "utf8"))) offenders.push(`${f.slice(here.length + 1)}: ${o}`);
  assert.deepEqual(offenders, []);
});
