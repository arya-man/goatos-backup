// guard: approvals-row-anatomy (TR-2 P1-5). The /approvals queue keeps the template order-list
// anatomy at sm+: TableCell padding 16px (a bare responsive `{ xs: 0 }` padding applies at EVERY
// width and collapsed the rows to 30px), the OrderTableRow two-line date cell, and the template
// TablePaginationCustom footer (TablePaginationLinks with a Dense switch and rows per page) ALWAYS
// under the table -- rows-per-page capped at the server's 20 (domain.MaxApprovalPageSize).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

export function rowAnatomyFindings(table, page, copy) {
  const out = [];
  const cellRule = /"& > td":\s*\{[^\n]*\}/.exec(table)?.[0] ?? "";
  if (/\bp:\s*\{\s*xs:\s*0\s*\}/.test(cellRule) || !/\bp:\s*\{\s*xs:\s*0,\s*sm:\s*2\s*\}/.test(cellRule)) out.push("cells lose the template 16px padding at sm+");
  if (!/<ListItemText[\s\S]{0,80}primary=\{fmtDate\(item\.raised_at\)\}/.test(table)) out.push("date cell is not the template two-line ListItemText");
  if (/cursor \|\| nextCursor \?/.test(page)) out.push("pager only renders when there is a second page");
  if (!/<TablePaginationLinks[\s\S]*rowsPerPageHrefs=[\s\S]*left=\{(?:<Box[^>]*>)?<DenseToggleAuto/.test(page)) out.push("pager lacks rows-per-page or the Dense switch");
  const sizes = /PAGE_SIZES = \[([^\]]*)\]/.exec(copy)?.[1].split(",").map(Number) ?? [];
  if (!sizes.length || sizes.some((n) => n > 20)) out.push("rows-per-page past the server cap of 20");
  return out;
}

test("approvals queue keeps the template row + pager anatomy", () => {
  assert.deepEqual(rowAnatomyFindings(read("./approvals-queue-table.tsx"), read("./approvals-page.tsx"), read("./copy.ts")), []);
});

test("self-test: the 30px-row version is caught", () => {
  const table = `"& > td": { display: { xs: "block", sm: "table-cell" }, p: { xs: 0 }, minWidth: 0 },\n{fmtDateTime(item.raised_at)}`;
  const page = `footer={cursor || nextCursor ? (<TablePaginationLinks page={0} />) : null}`;
  assert.equal(rowAnatomyFindings(table, page, "PAGE_SIZES = [10, 25, 50]").length, 5);
});
