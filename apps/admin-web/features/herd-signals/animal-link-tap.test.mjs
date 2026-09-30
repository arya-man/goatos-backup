import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

// guard: herd-signals-animal-tap (TR1-#8). The /herd-signals animal link measured 132x21 at 390: its sx
// asks for a 44px phone box, but legacy rules (frame.css `min-height:0`, mesha-theme.css
// `min-height:40px`) on `td:first-child > a` beat it. No legacy stylesheet may set the link's height.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("the animal link keeps its 44px phone box", () => {
  const table = read("./herd-signals-table.tsx");
  assert.match(table, /minHeight: \{ xs: TAP_MIN, md: "auto" \}/);
  for (const css of ["frame", "mesha-theme"]) {
    const src = legacyCss(css);
    const rules = src.match(/[^{}]*hs-selectable td:first-child > a[^{}]*\{[^}]*\}/g) || [];
    for (const rule of rules) assert.doesNotMatch(rule, /[{;]\s*(min-|max-)?height:/, `${css}: ${rule.trim()}`);
  }
});
