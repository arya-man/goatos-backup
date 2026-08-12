// The Verify shed picker must say WHICH PARK a shed belongs to.
//
// A shed NAME is not unique across the farm: Castro, Gandhi, Godel 1, Godel 2, Mandela 1,
// Mandela 2 and Yashoda each exist in BOTH parks. On STG, 2026-08-12, that made nine of the
// sixty-seven shed options exact duplicate labels, sitting adjacent under the backend's
// ORDER BY — two "Castro - 1" rows with nothing to separate them. The option VALUE was always
// the right shed (the id is a UUID, never a name), so filtering worked; what a reader could not
// do was tell which one she was picking, and the park holding more pens read as the only park
// with any work at all.
//
// The park is carried BESIDE the label and grouped, never concatenated into it: the label is the
// shed's operational location and `oploc` owns that string (AGENTS.md → Rule 5).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");

test("the shed picker groups options by park", () => {
  assert.match(pageSource, /<optgroup key=\{parkLabel\} label=\{parkLabel\}>/, "shed options must render inside a per-park <optgroup>");
  assert.match(pageSource, /shedsByPark/, "the page must derive the park grouping");
});

test("the park is never concatenated into the shed's operational-location display", () => {
  // The one thing that would silently undo Rule 5: building "Coimbatore · Castro - 1" here
  // instead of grouping. The option's text must remain exactly what the backend composed.
  const optionText = pageSource.match(/<option key=\{option\.id\} value=\{option\.id\}>[\s\S]{0,160}?<\/option>/g) ?? [];
  assert.ok(optionText.length >= 2, "expected the grouped and ungrouped option renderers");
  for (const rendered of optionText) {
    assert.match(
      rendered,
      /\{option\.operational_location_display \|\| option\.label\}/,
      "an option's text must be the backend-composed display verbatim",
    );
    assert.doesNotMatch(rendered, /park_label/, "the park must not be folded into the option text");
  }
});

test("a shed whose park the backend could not resolve is still offered", () => {
  // The backend sends no park when an option's rows disagree about one, rather than guessing.
  // Dropping those options would hide a filter that still works correctly.
  assert.match(
    pageSource,
    /shedsWithoutPark = sheds\.filter\(\(option\) => !option\.park_label\)/,
    "park-less options must be kept and rendered outside the groups",
  );
  const ungroupedAt = pageSource.indexOf("shedsWithoutPark.map");
  const groupedAt = pageSource.indexOf("shedsByPark.map");
  assert.ok(ungroupedAt > 0 && groupedAt > ungroupedAt, "ungrouped options render before the park groups");
});

test("grouping preserves the backend's order instead of re-sorting", () => {
  // The backend already returns options park-first (ORDER BY park_label, shed_label, …). Sorting
  // again here would be a second opinion about a list it already ordered — and the two would drift.
  const derivation = pageSource.slice(pageSource.indexOf("const shedsByPark"), pageSource.indexOf("const shedsByPark") + 600);
  assert.doesNotMatch(derivation, /\.sort\(/, "the page must not re-sort the backend's shed order");
});
