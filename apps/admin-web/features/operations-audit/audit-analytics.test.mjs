// guard: audit-strip-no-simplebar (FIXJ4, /operations/audit "tab moved 108px"). The analytics strip
// above the status tabs is re-keyed on every tab / filter click; the template Scrollbar (SimpleBar)
// sizes its content from JS and was 0px tall for the first frame, so the tabs jumped. The strip
// scrolls sideways in a plain overflow box instead.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("audit-strip-no-simplebar: the analytics strip never mounts the SimpleBar Scrollbar", () => {
  const src = readFileSync(new URL("./audit-analytics.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(src, /<Scrollbar\b|components\/minimal\/scrollbar/);
  assert.match(src, /overflowX: "auto"/);
});
