import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// pr294 L-A11: the empty Pending tab's pager read a bare "0" with nothing after it.
test("approvals pager range names what an empty tab holds", () => {
  const page = readFileSync(new URL("./approvals-page.tsx", import.meta.url), "utf8");
  const copy = readFileSync(new URL("./copy.ts", import.meta.url), "utf8");
  assert.doesNotMatch(page, /rangeLabel=\{items\.length === 0 \? "0"/);
  assert.match(page, /rangeLabel=\{items\.length === 0 \? COPY\.pager\.none/);
  assert.match(copy, /none: "0 requests"/);
});

// pr294 L-A11: the Action Center lane board is not a table, so its pager offers no Dense switch.
test("action center board pager carries no density switch", () => {
  const ac = readFileSync(new URL("../process-integrity/action-center.tsx", import.meta.url), "utf8");
  assert.match(ac, /hrefForPage=\{boardPagerHref\}\s*hrefForPageSize=\{boardPageSizeHref\}\s*dense=\{false\}/);
  const pager = readFileSync(new URL("../preventive-care-vaccination/table-pager.tsx", import.meta.url), "utf8");
  assert.match(pager, /left=\{left \?\? \(dense && total > 10 \?/);
});
