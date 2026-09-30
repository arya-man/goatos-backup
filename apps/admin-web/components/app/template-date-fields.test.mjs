// Guard: template-date-fields (TR1-#22). Filter-bar dates are the template's MUI X DatePicker (label on
// the border, trailing calendar icon, DD/MM/YYYY): a single day in WorklistFilters, a start / end pair
// in DateRangeField. No custom calendar inside an outlined TextField (inputComponent) any more.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const worklist = readFileSync(new URL("../worklist-filters.tsx", import.meta.url), "utf8");
const range = readFileSync(new URL("./date-range-field.tsx", import.meta.url), "utf8");

test("template-date-fields: DatePicker, not a themed calendar in a TextField", () => {
  assert.match(worklist, /<DatePicker\b[\s\S]{0,200}format="DD\/MM\/YYYY"/);
  assert.doesNotMatch(worklist, /ThemedDateInput|inputComponent: ThemedDate/);
  assert.equal((range.match(/<DatePicker\b/g) ?? []).length, 2, "start + end pickers");
  assert.match(range, /format="DD\/MM\/YYYY"/);
  assert.doesNotMatch(range, /inputComponent|ThemedDatePicker/);
});

// FIXJ7 (J1B P2-5): the /sales payment row edited its received date in the browser's native
// `<TextField type="date">` picker (the last one, waived as native-date-input). It is the template
// DatePicker through ThemedDatePicker (`form` posts the ISO day with the row's hidden edit form).
// No feature or component renders a native date / datetime / month / week input.
import { readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const appRoot = fileURLToPath(new URL("../../", import.meta.url));
function tsxFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const abs = join(dir, e.name);
    if (e.isDirectory()) return e.name === "minimal" || e.name === "node_modules" ? [] : tsxFiles(abs);
    return /\.tsx$/.test(e.name) && !/\.(test|stories)\.tsx$/.test(e.name) ? [abs] : [];
  });
}

test("no native date input anywhere in app / components / features", () => {
  const offenders = [];
  for (const dir of ["app", "components", "features", "layouts"]) {
    for (const abs of tsxFiles(join(appRoot, dir))) {
      const src = readFileSync(abs, "utf8");
      if (/\btype=["'{]\s*["']?(?:date|datetime-local|month|week)["']/.test(src)) offenders.push(abs.slice(appRoot.length));
    }
  }
  assert.deepEqual(offenders, [], `native date inputs: ${offenders.join(", ")}`);
});

test("the /sales payment row date is the template picker, posting with its edit form", () => {
  const drawer = readFileSync(new URL("../../features/procurement/sales-record-drawer.tsx", import.meta.url), "utf8");
  assert.match(drawer, /<ThemedDatePicker\s+name="received_on"\s+form=\{editFormId\}/);
  const picker = readFileSync(new URL("../themed-date-picker.tsx", import.meta.url), "utf8");
  assert.match(picker, /type="hidden" name=\{name\} value=\{selected\} form=\{form\}/);
});
