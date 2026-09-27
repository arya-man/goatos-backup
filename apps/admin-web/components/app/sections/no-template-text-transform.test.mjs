// guard: no-template-text-transform (REVIEW-28 O36). Template text transforms (capitalize,
// uppercase) must never rewrite Mesha copy ("All items" -> "All Items", "pH" -> "PH"). In a
// template-derived section every sx that carries a textTransform is wrapped in mergeSx(…, slotProps?.x)
// so callers can pass textTransform: "none", and every caller of MailNavItem does.
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const dir = new URL(".", import.meta.url).pathname;
const walk = (d, out = []) => { for (const n of readdirSync(d)) { const a = join(d, n); if (statSync(a).isDirectory()) walk(a, out); else if (n.endsWith(".tsx")) out.push(a); } return out; };

test("every textTransform in a derived section sits in an overridable sx (mergeSx)", () => {
  const offenders = [];
  for (const f of walk(dir)) {
    const src = readFileSync(f, "utf8");
    for (const m of src.matchAll(/textTransform:\s*'(capitalize|uppercase)'/g)) {
      const before = src.slice(Math.max(0, m.index - 400), m.index);
      const lastSx = before.lastIndexOf("sx=");
      if (lastSx < 0 || !/sx=\{mergeSx\(/.test(before.slice(lastSx))) offenders.push(`${f.slice(dir.length)}: ${m[0]}`);
    }
  }
  assert.deepEqual(offenders, [], "wrap it in mergeSx(template, slotProps?.x) and have callers pass textTransform: 'none'");
});

test("MailNavItem callers keep Mesha copy as written", () => {
  for (const f of ["catalogue-lists.tsx", "items-page.tsx", "register-preview.tsx"]) {
    const src = readFileSync(new URL(`../../../features/configuration/${f}`, import.meta.url), "utf8");
    for (const m of src.matchAll(/<MailNavItem\b[\s\S]*?\/>/g)) assert.match(m[0], /label: \{ textTransform: "none" \}/, `${f}: ${m[0].slice(0, 60)}`);
  }
});
