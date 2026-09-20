import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { blankQuestion, emitVendorForm, parseVendorForm, vendorFormProblems } from "./inspection-model.ts";

// VENDOR FORM (2026-09-19): the seeded document is the same bytes migration 000364 publishes
// (pinned by TestMigrationEmbedsTheSeededVendorForm). Opening the editor and publishing with no
// edits must reproduce it exactly, or every re-publish would silently change the form.
const seedPath = new URL("../../../../backend/internal/procurement/domain/vendorformseed/vendor.json", import.meta.url);

function canonical(v) {
  return JSON.stringify(v, (_k, val) => (val && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val));
}

test("the seeded vendor form round-trips through the editor model byte-faithfully", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseVendorForm({ vendor_form: doc });
  assert.ok(rows);
  assert.equal(rows.pages.length, 3);
  assert.equal(rows.loadForm.length, 0);
  assert.equal(rows.pages[0].questions[1].catalog, "record_type");
  assert.deepEqual(vendorFormProblems(rows), []);
  assert.equal(canonical(emitVendorForm(rows)), canonical(doc));
});

test("an inspection document is not a vendor form and vice versa", () => {
  assert.equal(parseVendorForm({ inspection: { pages: [] } }), null);
});

test("the pre-checks refuse what the register cannot run", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseVendorForm({ vendor_form: doc });
  rows.pages[0].questions[0].required = false; // business_name optional
  assert.ok(vendorFormProblems(rows).some((p) => p.includes('"business_name" must stay compulsory')));
  rows.pages[0].questions[0].required = true;
  const media = { ...blankQuestion("media"), key: "pic", title: "Photo" };
  rows.pages[2].questions.push(media);
  assert.ok(vendorFormProblems(rows).some((p) => p.includes("takes a choice, text or number")));
  rows.pages[2].questions.pop();
  // An added pick-one with choices and a conditional number is fine.
  rows.pages.push({ id: "p", key: "extra", title: "Extra", hint: "", questions: [
    { ...blankQuestion("choice"), key: "transport", title: "Own transport?" },
    { ...blankQuestion("number"), key: "vehicles", title: "Vehicles", onlyIfQuestion: "transport", onlyIfValue: "yes" },
  ] });
  assert.deepEqual(vendorFormProblems(rows), []);
  const emitted = emitVendorForm(rows);
  assert.equal(emitted.pages[3].questions[1].only_if.value, "yes");
});
