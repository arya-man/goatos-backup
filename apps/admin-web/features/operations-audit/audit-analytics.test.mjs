// guard: audit-strip-no-simplebar (FIXJ4, /operations/audit "tab moved 108px"). The analytics strip
// above the status tabs is re-keyed on every tab / filter click; the template Scrollbar (SimpleBar)
// sizes its content from JS and was 0px tall for the first frame, so the tabs jumped. The strip
// scrolls sideways in a plain overflow box instead.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("audit-strip-no-simplebar: the analytics strips above URL-keyed tabs never mount the SimpleBar Scrollbar", () => {
  for (const rel of ["./audit-analytics.tsx", "../operations-dlq/dlq-analytics.tsx"]) {
    const src = readFileSync(new URL(rel, import.meta.url), "utf8");
    assert.doesNotMatch(src, /<Scrollbar\b|components\/minimal\/scrollbar/, rel);
    assert.match(src, /overflowX: "auto"/, rel);
  }
});
