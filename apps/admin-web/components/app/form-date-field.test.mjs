import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: form-date-field-template (J2 P1-6). A form date is the template MUI X DatePicker with its
// label floating over the outlined field (DD/MM/YYYY, trailing calendar icon), posting the ISO day
// under `name`; a required one refuses the submit with the backend copy.
const src = readFileSync(new URL("./form-date-field.tsx", import.meta.url), "utf8");

test("guard: form-date-field-template - MUI X DatePicker, floating label, ISO hidden value, submit refusal", () => {
  assert.match(src, /from "@mui\/x-date-pickers\/DatePicker"/);
  assert.match(src, /format="DD\/MM\/YYYY"/);
  assert.match(src, /<input ref=\{anchorRef\} type="hidden" name=\{name\} value=\{iso\} \/>/);
  assert.match(src, /inputLabel: \{ shrink: true, required \}/);
  assert.match(src, /event\.preventDefault\(\);\s*setError\(invalidDateText/);
  assert.doesNotMatch(src, /lucide-react|\.css"/);
});
