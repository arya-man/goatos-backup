import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Two defects on the same figure, found together on the sales month charts (2026-09-20):
//   1. a month with a MEASURED zero carried a month label and nothing above it, which reads as a
//      figure that failed to load rather than as "the farm sold none";
//   2. on a phone the TALLEST column's figure vanished -- the one month a reader most wants.
// Both are pinned here because both are one-line regressions to reintroduce.

const monthColumns = readFileSync(new URL("./month-columns.tsx", import.meta.url), "utf8");
const groupedColumns = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

test("a measured zero still prints its figure; only the bar is dropped", () => {
  // The figure is gated on the DISPLAY string, never on the value -- gating it on `value > 0`
  // is exactly the defect. The bar stays gated on the value, because zero has no height.
  assert.match(monthColumns, /\{datum\.display \? \(\s*<span className="mcstack">/);
  assert.match(monthColumns, /\{datum\.value > 0 \? \(\s*<span className="mcbar"/);
  assert.doesNotMatch(
    monthColumns,
    /\{datum\.value > 0 \? \(\s*<span className="mcstack">/,
    "a zero column must still carry its figure",
  );

  // The grouped chart keeps absent and zero apart: a measured zero prints, an absent value does
  // not, because "cost not recorded" is not a quantity anyone measured.
  assert.match(groupedColumns, /const zeroLabel = value === 0 \? barLabelFor\(datum, i\) : null;/);
  assert.match(groupedColumns, /if \(!zeroLabel\) \{[\s\S]*?className="gcb gcempty"/, "an absent value keeps its hidden placeholder");
});

test("the column figure never shrinks, so the tallest bar keeps its number on a phone", () => {
  // The tallest bar fills the stack's content box, so its figure overflows into the reserved
  // padding. As an ordinary flex item it is crushed to zero height there -- and on mobile `.mcv`
  // clips its own overflow, so the crush hid it outright.
  const rule = (name) => {
    const match = css.match(new RegExp(`\\n\\.${name}\\{([^}]*)\\}`));
    assert.ok(match, `mesha-theme.css must declare .${name}`);
    return match[1];
  };
  for (const name of ["mcv", "gcval"]) {
    assert.match(rule(name), /flex:0 0 auto/, `.${name} must not shrink`);
  }
});

test("a grouped column on a phone is wide enough for one bar per series", () => {
  // Four series at a 16px bar plus their figures need ~110px. The desktop floor is 64px, which is
  // where the figures started printing over the NEXT column's bars. Sized on the component, not on
  // one page: Counts Breakdown draws the same chart.
  const phone = css.match(/@media\(max-width:680px\)\{\s*\n\s*\.gcol\{[^}]*min-width:(\d+)px/);
  assert.ok(phone, "the grouped column must carry a phone width of its own");
  assert.ok(Number(phone[1]) >= 92, `a phone column of ${phone[1]}px is narrower than its own bars`);
});

test("the room reserved above the bars still fits the figure at every width", () => {
  // padding-top on the stack is what keeps the figure inside the chart's own box; the scroll
  // container clips anything above it. Checked against the figure's own font size and gap at each
  // breakpoint so a font bump cannot silently clip the number again.
  const reserves = [
    { pad: /\.mcstack\{[^}]*padding-top:(\d+)px/, font: /\n\.mcv\{[^}]*font-size:([\d.]+)px/, gap: 4 },
    { pad: /\.mcstack\{padding-top:(\d+)px\}/, font: /\.mcv\{font-size:([\d.]+)px/, gap: 4 },
  ];
  for (const { pad, font, gap } of reserves) {
    const padMatch = css.match(pad);
    const fontMatch = css.match(font);
    assert.ok(padMatch && fontMatch, "both the reserve and the figure's size must be declared");
    assert.ok(
      Number(padMatch[1]) >= Number(fontMatch[1]) + gap,
      `reserve ${padMatch[1]}px must hold a ${fontMatch[1]}px figure plus its ${gap}px gap`,
    );
  }
});
