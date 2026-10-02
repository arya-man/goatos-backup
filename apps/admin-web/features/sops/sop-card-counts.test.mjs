import { test } from "node:test";
import assert from "node:assert/strict";
import { deriveStepCount, deriveToxinStepCount, deriveInspectionQuestionCount, toSopView } from "./sop-derive.ts";

// CARD COUNTS (2026-09-20): every document on /procurement/sops carries an EMPTY `fields` array
// because its content lives in a module-owned section. The library card must say what the
// document actually holds -- and must not say "0 steps" about a form that has none to hold.

const page = (n) => ({ questions: Array.from({ length: n }, (_, i) => ({ id: `q${i}` })) });

test("a fieldless module-owned document reports no step count at all", () => {
  assert.equal(deriveStepCount({ fields: [], vendor_form: { pages: [page(4)] } }), null);
  assert.equal(deriveStepCount({ fields: [], feed_purchase_form: { pages: [page(4)] } }), null);
  assert.equal(deriveStepCount({ fields: [], follow_up: { tracks: [] } }), null);
  assert.equal(deriveStepCount({ fields: [], inspection: { pages: [] } }), null);
});

test("the aflatoxin procedure reports its own authored steps", () => {
  const toxin = { fields: [], toxin: { steps: Array.from({ length: 7 }, (_, i) => ({ step_no: i + 1 })) } };
  assert.equal(deriveToxinStepCount(toxin), 7);
  assert.equal(deriveStepCount(toxin), 7);
});

test("a capture document still counts its own fields", () => {
  assert.equal(deriveStepCount({ fields: [{ id: "a" }, { id: "b" }] }), 2);
  assert.equal(deriveStepCount({ fields: [] }), 0);
  assert.equal(deriveStepCount({}), null);
  assert.equal(deriveToxinStepCount({ fields: [] }), 0);
});

// The card, not the helper: the helper always took a section name, and the defect was that the
// view never asked it for this one -- so the feed purchase form's card carried no question count.
const card = (form_dsl) =>
  toSopView({ sop_id: "s", code: "procurement.feed_purchase_form", name: "Feed purchase form", description: "", status: "published" },
             { sop_version_id: "v", version: 2, status: "published", form_dsl, proof_policy: {}, row_version: 1 });

test("the feed purchase form's card counts its questions like every other entry form's", () => {
  const dsl = { fields: [], feed_purchase_form: { pages: [page(5), page(9)] } };
  assert.equal(deriveInspectionQuestionCount(dsl, "feed_purchase_form"), 14);
  assert.equal(card(dsl).inspectionQuestionCount, 14);
  assert.equal(card(dsl).stepCount, null);
  // and a supplier form still counts, so the widening did not replace one section with another
  assert.equal(card({ fields: [], vendor_form: { pages: [page(19)] } }).inspectionQuestionCount, 19);
});

import { slugKey, slugValue } from "./inspection-model.ts";

// A CHOICE'S VALUE IS AN ANSWER, NOT AN IDENTIFIER (2026-09-20). Authoring "How do we pay them?"
// with Advance / On delivery / 30 days published `days` for the third -- the leading number, the
// only thing that said which term it was, silently dropped.
test("a choice value keeps the number the label starts with", () => {
  assert.equal(slugValue("30 days", new Set()), "30_days");
  assert.equal(slugValue("60 days", new Set(["30_days"])), "60_days");
  assert.equal(slugValue("Advance", new Set()), "advance");
  assert.equal(slugValue("On delivery", new Set()), "on_delivery");
  // two labels that really do collide still disambiguate rather than overwriting
  assert.equal(slugValue("Advance", new Set(["advance"])), "advance_2");
  assert.equal(slugValue("!!", new Set()), "choice");
});

test("a question KEY still refuses to start with a digit, because it is an identifier", () => {
  assert.equal(slugKey("30 days", new Set()), "days");
});

import { readFileSync } from "node:fs";

test("the editor derives a choice's value with slugValue, never with the key slug", () => {
  const src = readFileSync(new URL("./inspection-editor.tsx", import.meta.url), "utf8");
  assert.match(src, /o\.value === "other" \? "other" : slugValue\(label, others, "choice"\)/,
    "a choice value must keep a leading number; slugKey drops it");
  assert.doesNotMatch(src, /: slugKey\(label, others, "choice"\)/);
});

import { vendorFormProblems, VENDOR_REQUIRED_KEYS, FEED_PURCHASE_REQUIRED_KEYS } from "./inspection-model.ts";

// THE PRE-CHECKS BELONG TO THE PROFILE (2026-09-20). Both entry forms run through
// vendorFormProblems, and while it named the vendor register's keys outright, the feed purchase
// form could not be published from the editor at all -- Publish stayed disabled, demanding
// `business_name` of a form that records a feed load.
const q = (key, title, extra = {}) => ({ key, title, kind: "text", required: true, options: [], min: "", max: "", unit: "", hint: "", catalog: "", allowOther: false, onlyIfQuestion: "", onlyIfValue: "", ...extra });
const rowsOf = (...keys) => ({ loadForm: [], pages: [{ key: "p1", title: "Page 1", hint: "", questions: keys.map((k) => q(k, k)) }] });

test("the feed purchase form is judged on the feed ledger's own compulsory columns", () => {
  const feedRows = rowsOf(...FEED_PURCHASE_REQUIRED_KEYS);
  assert.deepEqual(vendorFormProblems(feedRows, FEED_PURCHASE_REQUIRED_KEYS, "A feed load"), []);
  // the same document judged as a VENDOR form is refused, which is what was happening on screen
  const asVendor = vendorFormProblems(feedRows, VENDOR_REQUIRED_KEYS, "A vendor");
  assert.ok(asVendor.some((m) => m.includes("business_name")), "expected the vendor rules to refuse a feed form");
});

test("a feed load missing its own compulsory column is still refused, and says so as a feed load", () => {
  const short = rowsOf("purchase_date", "farm_label", "feed_item_label", "quantity_kg");
  const problems = vendorFormProblems(short, FEED_PURCHASE_REQUIRED_KEYS, "A feed load");
  assert.ok(problems.some((m) => m.includes('A feed load cannot exist without "vendor"')), problems.join(" | "));
});

test("the supplier form keeps the register's rules by default", () => {
  assert.deepEqual(vendorFormProblems(rowsOf(...VENDOR_REQUIRED_KEYS)), []);
  assert.ok(vendorFormProblems(rowsOf("business_name")).some((m) => m.includes("record_type")));
});

// ONE NAME PER COUNT ACROSS MODULES (PR #294 round 2): form fields are form questions on every
// library (Milk read "14 steps" beside Herd Operations' "form questions"); only the aflatoxin
// procedure's authored steps are "steps".
test("only a procedure's authored steps count as steps; form fields never do", () => {
  const view = (code, form_dsl) =>
    toSopView({ sop_id: "s", code, name: "x", description: "", status: "published" },
              { sop_version_id: "v", version: 1, status: "published", form_dsl, proof_policy: {}, row_version: 1 });
  const milk = view("milk.preparation", { fields: Array.from({ length: 14 }, (_, i) => ({ id: `f${i}` })) });
  assert.equal(milk.stepCount, 14);
  assert.equal(milk.stepCountIsProcedure, false);
  const toxin = view("procurement.toxin_test", { fields: [], toxin: { steps: Array.from({ length: 7 }, (_, i) => ({ step_no: i + 1 })) } });
  assert.equal(toxin.stepCount, 7);
  assert.equal(toxin.stepCountIsProcedure, true);
});
