// Guard: table-scroll-template (TR1-#20). A table wider than its card scrolls inside the template
// TableContainer or Scrollbar (thin visible scrollbar, theme scrollbar mixin), never a bare
// `<Box sx={{ overflowX: "auto" }}>` / `style={{ overflowX: "auto" }}` div around a <Table>. Files
// that still had the bare wrapper when the guard landed are listed with their count and may only
// shrink (their routes are owned by other workers); any new instance fails.
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const appRoot = fileURLToPath(new URL("../..", import.meta.url));

/** Bare overflow wrappers whose next ~6 lines open a <Table. */
export function bareTableScrollers(source) {
  const lines = source.split("\n");
  let n = 0;
  lines.forEach((line, i) => {
    if (!/<(Box|div)\b/.test(line)) return;
    const open = lines.slice(i, i + 8).join("\n");
    const tag = open.slice(0, open.indexOf(">", open.search(/overflowX: "auto"|overflow-x/)) + 1);
    if (!/overflowX: "auto"/.test(tag)) return;
    if (/<Table\b/.test(lines.slice(i, i + 10).join("\n"))) n += 1;
  });
  return n;
}

// Shrink-only allowance (2026-09-28). Remove an entry when its file is converted.
const ALLOWED = {
  "features/control-tower/index.tsx": 2,
  "features/counts/counts-breakdown-loads.tsx": 1,
  "features/counts/herd-actions-ui.tsx": 5,
  "features/counts/herd-passport-vaccination.tsx": 2,
  "features/health/health-types.tsx": 2,
  "features/people/notification-matrix.tsx": 2,
  "features/preventive-care-vaccination/cohort-detail.tsx": 1,
  "features/preventive-care-vaccination/command-board-view.tsx": 11,
  "features/preventive-care-vaccination/status-matrix.tsx": 1,
  "features/preventive-care-vaccination/supplier-warmup-context.tsx": 1,
  "features/procurement/load-forms.tsx": 1,
  "features/vaccination-sheds/shed-detail.tsx": 4,
};

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.tsx$/.test(name) && !/\.stories\.tsx$/.test(name)) out.push(abs);
  }
  return out;
}

test("table-scroll-template: self-test", () => {
  assert.equal(bareTableScrollers(`<Box sx={{ overflowX: "auto" }} tabIndex={0}>\n  <Table>`), 1);
  assert.equal(bareTableScrollers(`<div className="x" style={{ overflowX: "auto", marginTop: 8 }}>\n  <Table>`), 1);
  assert.equal(bareTableScrollers(`<TableContainer data-scroll-x="">\n  <Table>`), 0);
  assert.equal(bareTableScrollers(`<Box sx={{ overflowX: "auto" }}>\n  <Tabs />`), 0);
});

test("table-scroll-template: no new bare overflow wrapper around a table", () => {
  const over = [];
  for (const abs of walk(join(appRoot, "features"))) {
    const rel = relative(appRoot, abs);
    const n = bareTableScrollers(readFileSync(abs, "utf8"));
    if (n > (ALLOWED[rel] ?? 0)) over.push(`${rel}: ${n} bare overflow wrapper(s) around a <Table> (allowed ${ALLOWED[rel] ?? 0}); use TableContainer or Scrollbar`);
  }
  assert.deepEqual(over, []);
});
