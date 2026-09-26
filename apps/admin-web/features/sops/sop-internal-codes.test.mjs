import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sop-no-internal-codes (FJ3 P1-13, OCI smoke #19/#42). The SOP editors printed each
// capture/question key ("return_to_pen", "feed_transport_video", "purchase_date") and each choice's
// stored value beside the human label, and the SOP detail dialog printed "Code counts.reconcile"
// and "type: animal_id_scan". Keys stay in the data (the phones stamp uploads with them); a person
// reads the title, the choice label and a worded field type.
const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("SOP editors do not print internal keys or choice values", () => {
  for (const file of ["./feed-editor.tsx", "./shifting-editor.tsx", "./weighing-editor.tsx", "./inspection-editor.tsx", "./pc-care-editor.tsx", "./capture-editor.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, /<code[^>]*>\{(?:slot|p|q|row)\.key\}<\/code>/, `${file} prints a key`);
    assert.doesNotMatch(src, /<code[^>]*>\{o\.value\}<\/code>/, `${file} prints a choice value`);
  }
});

test("SOP detail dialog shows no SOP code and words the field type; full screen on a phone", () => {
  const lib = read("./sop-library.tsx");
  assert.doesNotMatch(lib, /\{view\.code\}<\/div>/, "the detail dialog prints the SOP code");
  assert.doesNotMatch(lib, /label\.type"\)\}: \{f\.type\}/, "the detail dialog prints a raw field type");
  assert.match(lib, /<Dialog fullWidth fullScreen=\{fullScreen\} maxWidth="md"/);
});

// guard: sop-editor-template-fields (FJ3 P1-16). Editor fields are MUI outlined TextFields with
// their own label (no label-above `.numlbl` wrapper beside a floating-label select), and the legacy
// `.qcard input` paint (border, padding, background) must not reach the MUI input inside them —
// it drew a second box inside every outlined field.
test("SOP editor text fields carry their own MUI label and escape the legacy .qcard input paint", () => {
  for (const file of ["./feed-editor.tsx", "./followup-editor.tsx", "./shifting-editor.tsx"]) {
    assert.doesNotMatch(read(file), /<label className="numlbl">\s*\{copy\([^}]*\)\}\s*<MuiTextField/, `${file}: label-above wrapper around a TextField`);
  }
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  const rule = css.match(/\.qcard input[^{]*\{/);
  assert.ok(rule && /:not\(\.MuiInputBase-input\)/.test(rule[0]), ".qcard input rule must exclude MUI inputs");
});

// guard: sop-flow-phone-fit (FJ3 P1-17): on a phone the flow opened at ~41% hugging the right edge.
test("SOP flow canvas keeps a 70% floor on a phone and centres the scaled flow", () => {
  const canvas = read("./flow-canvas.tsx");
  assert.match(canvas, /el\.clientWidth < 600 \? 0\.7 : 0\.35/);
  assert.match(canvas, /width: layout\.width \* zoom, height: layout\.height \* zoom, margin: "0 auto"/);
});
