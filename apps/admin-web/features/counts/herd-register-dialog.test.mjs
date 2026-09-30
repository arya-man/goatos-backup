import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: herd-register-dialog-form (J2 P1-6). The Register animal dialog is the template product
// form: one grid of outlined fields whose labels float over the control. Dates are FormDateField
// (MUI X DatePicker, floating label, DD/MM/YYYY), never a caption above a button whose placeholder
// repeats it ("DOB" / "DOB"); required selects show the asterisk like required text fields; no
// disabled submit.
const src = readFileSync(new URL("./herd-actions-ui.tsx", import.meta.url), "utf8");
const register = src.slice(src.indexOf("function RegisterGoatDrawer("), src.indexOf("function RegisterShedDrawer("));

test("guard: herd-register-dialog-form - template form fields, dates on FormDateField", () => {
  assert.ok(register.length > 200, "RegisterGoatDrawer found");
  assert.doesNotMatch(src, /function DateField\(|<ThemedDatePicker/, "no caption-above date wrapper, no summary-button picker");
  assert.equal((register.match(/<FormDateField/g) ?? []).length, 2, "DOB and entry date are FormDateField");
  assert.doesNotMatch(register, /<input(?![^>]*type="hidden")/, "no native visible inputs");
  assert.doesNotMatch(src, /disabled aria-disabled="true"/, "no dead Register animal / Register pen button");
  const select = readFileSync(new URL("../../components/form-select.tsx", import.meta.url), "utf8");
  assert.match(select, /inputLabel: \{ shrink: true, required \}/, "required selects show the label asterisk");
});

test("guard: herd-register-dialog-form - the backend copy says required once (asterisk), not in the label", () => {
  const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  for (const key of ["field.park_required", "field.shed_required", "field.entry_date_required", "field.shed_name_required"]) {
    const m = service.match(new RegExp(`"${key.replace(/\./g, "\\.")}":\\s*"([^"]*)"`));
    assert.ok(m, `${key} present`);
    assert.doesNotMatch(m[1], /\(required\)/, `${key} must not spell "(required)"; the field shows the asterisk`);
  }
  assert.doesNotMatch(service, /"option\.optional":\s*"— optional —"/);
});
