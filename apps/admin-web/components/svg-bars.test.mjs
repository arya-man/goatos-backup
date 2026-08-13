import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./svg-bars.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

/** Re-derives the geometry from the component's own constants, so this test cannot go stale. */
function geometry() {
  const num = (name) => {
    const match = source.match(new RegExp(`const ${name} = (\\d+);`));
    assert.ok(match, `svg-bars.tsx must declare ${name}`);
    return Number(match[1]);
  };
  const rowHeight = num("ROW_HEIGHT");
  const rowGap = num("ROW_GAP");
  const visible = num("VISIBLE_BARS");
  return {
    wide: num("WIDE_VIEW_WIDTH"),
    narrow: num("NARROW_VIEW_WIDTH"),
    visible,
    // Mirrors barsViewHeight().
    height: visible * (rowHeight + rowGap) + 4,
  };
}

test("the scroll window is sized by ratio, never by a fixed height", () => {
  // The SVG has no height attribute — it is scaled by its viewBox, so its rendered height is
  // containerWidth × viewBoxHeight / viewBoxWidth. A max-height in px would hold ten rows at one
  // card width and six at another, which is the whole reason this is an aspect-ratio.
  assert.match(source, /viewBox=\{`0 0 \$\{viewWidth\} \$\{height\}`\}/);
  assert.doesNotMatch(source, /height=\{height\}/, "a height attribute would break the ratio contract");
  assert.match(css, /\.svgbars-scroll\{[^}]*aspect-ratio:/);
  assert.doesNotMatch(css, /\.svgbars-scroll\{[^}]*max-height:/);
});

test("the CSS ratios match the component's own geometry, at BOTH scales", () => {
  // The narrow scale draws the same ten rows into a 280-wide viewBox. Reusing the wide ratio there
  // would show three bars on a phone, which is the failure this pins.
  const { wide, narrow, height } = geometry();
  assert.match(css, new RegExp(`\\.svgbars-scroll\\{[^}]*aspect-ratio:${wide} / ${height}\\}`));
  assert.match(css, new RegExp(`\\.svgbars-scroll\\{aspect-ratio:${narrow} / ${height}\\}`));
});

test("the window appears only when there is something to scroll to", () => {
  // Applied unconditionally it would stretch a three-bar chart to ten rows of empty card.
  assert.match(source, /const scrolls = bars\.length > VISIBLE_BARS;/);
  assert.match(source, /svgbars\$\{scrolls \? " svgbars-scroll" : ""\}/);
});

test("a scrolling chart is keyboard-reachable, a short one adds no tab stop", () => {
  assert.match(source, /tabIndex=\{scrolls \? 0 : undefined\}/);
});

test("ten bars stand in the card", () => {
  assert.equal(geometry().visible, 10);
});
