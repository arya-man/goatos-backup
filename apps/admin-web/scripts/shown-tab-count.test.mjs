import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: shown-tab-count (J3 P2-1). A tab strip whose counts the backend serves for the ACTIVE
// filter only puts the count Label on the shown tab, the same on every tab. /vaccination "Vaccination
// by pen" counted page-1 rows ("Overdue 25" for 83 overdue pens) and /operations/audit counted the
// other tabs from the All summary; both lost their Labels after a switch.
const read = (rel) => readFileSync(new URL(`../${rel}`, import.meta.url), "utf8");

test("guard: shown-tab-count - /vaccination pen status tabs carry the filtered total on the shown tab only", () => {
  const src = read("features/vaccination-sheds/shed-board.tsx");
  assert.doesNotMatch(src, /statusCounts/, "no counts summed from the rows of one page");
  assert.match(src, /count: statusFilter === s \? total : undefined/);
  assert.match(src, /count: !statusFilter \? total : undefined/);
});

test("guard: shown-tab-count - /operations/audit status tabs count the shown tab only", () => {
  const src = read("features/operations-audit/audit-log.tsx");
  assert.match(src, /const tabCount = \(key: string\): number \| undefined => \(summary && key === activeStatusTab\.key \? summary\.actions : undefined\);/);
});
