import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: summary-strip-no-empty-share (TR1-#35). /verify's header opens three side panels: all three
// are outlined secondary buttons (two green contained ones read as competing primary actions). The
// InvoiceAnalytic strips on /verify and /operations/dlq print a share caption only when there is a
// total to share (no "0%" repeated in every cell).
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
test("verify header panel buttons are outlined", () => {
  for (const f of ["./analytics-panel.tsx", "./video-log-panel.tsx", "./randomization-panel.tsx"]) {
    const src = read(f);
    const btn = src.slice(src.indexOf("<Button"), src.indexOf("</Button>"));
    assert.match(btn, /variant="outlined" color="inherit"/, f);
  }
});
test("summary strips print a share only with a total", () => {
  assert.match(read("./verification-review-page.tsx"), /caption=\{statusTotal \? fPercent\(100\) : undefined\}/);
  assert.match(read("../operations-dlq/index.tsx"), /total: total \? fPercent\(share\(cell\.count\)\) : "",/);
});
