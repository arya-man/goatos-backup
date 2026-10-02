import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: template-pager-footer (J2 P2-8). List footers are the template TablePagination
// (TablePaginationLinks: range readout "1–5" + arrows), never a bespoke "0 rows ⓘ" line with
// outlined Restart / Next buttons (/health/config) or a "Rows in view: 1" readout on the first page
// (/approvals).
const read = (rel) => readFileSync(new URL(`../${rel}`, import.meta.url), "utf8");

test("guard: template-pager-footer - /health/config and /approvals use the template pager", () => {
  const health = read("features/health/health-config.tsx");
  assert.match(health, /<TablePaginationLinks[\s\S]{0,400}count=\{-1\}/);
  assert.doesNotMatch(health, /<LinkButton href=\{restartHref\}/);
  const approvals = read("features/approvals/approvals-page.tsx");
  assert.match(approvals, /cursor \? `\$\{COPY\.kpi\.rowsInView\}: \$\{items\.length\}` : `1–\$\{items\.length\} \$\{COPY\.pager\.of\} \$\{items\.length\}/);
});
