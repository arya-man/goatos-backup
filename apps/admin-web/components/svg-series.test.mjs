import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./svg-series.tsx", import.meta.url), "utf8");

test("series charts keep axis date labels compact and explicit", () => {
  assert.match(source, /const fmtDay = \(label: string\) => \{/);
  assert.match(source, /\^\\d\{2\}\(\\d\{2\}\)-\(\\d\{2\}\)-\(\\d\{2\}\)\$/);
  assert.match(source, /return m \? `\$\{m\[3\]\}-\$\{m\[2\]\}-\$\{m\[1\]\}` : label;/);
  assert.match(source, /fontSize="8"[^>]*>\s*\{fmtDay\(days\[0\]\.label\)\}/s);
  assert.match(source, /fontSize="8"[^>]*>\s*\{fmtDay\(dayLabels\[0\]\)\}/s);
  assert.doesNotMatch(source, /fontSize="9"[^>]*>\s*\{dayLabels\[0\]\}/s);
});

test("series charts render interior ticks without crowding endpoints", () => {
  assert.match(source, /const interiorTickIdx = \(n: number\) => \{/);
  assert.match(source, /const step = Math\.ceil\(Math\.max\(1, n - 1\) \/ 5\);/);
  assert.match(source, /n - 1 - i >= Math\.max\(1, step \/ 2\)/);
  assert.match(source, /interiorTickIdx\(days\.length\)\.map/);
  assert.match(source, /interiorTickIdx\(dayLabels\.length\)\.map/);
  assert.match(source, /fontSize="7"[\s\S]*\{fmtDay\(days\[i\]\.label\)\}/);
  assert.match(source, /fontSize="7"[\s\S]*\{fmtDay\(dayLabels\[i\]\)\}/);
});

test("wide y-axis ticks expand plot padding instead of clipping", () => {
  assert.match(source, /const padForTicks = \(max: number\) => Math\.max\(PAD_X, Math\.ceil\(nf\(max\)\.length \* 5\.6\) \+ 10\);/);
  assert.match(source, /const padX = padForTicks\(max\);/);
  assert.match(source, /const slot = \(VIEW_W - padX - PAD_X\) \/ days\.length;/);
  // The line chart's right edge yields to a secondary axis only when one is drawn; a chart
  // without one keeps its 6px margin, so the secondary series can never squash a plain chart.
  assert.match(source, /const rightX = secondary \? VIEW_W - padForTicks\(secondaryMax\) : VIEW_W - 6;/);
  assert.match(source, /const stepX = \(rightX - padX\) \/ Math\.max\(1, dayLabels\.length - 1\);/);
  assert.match(source, /x=\{padX - 5\}/);
  assert.doesNotMatch(source, /x=\{PAD_X - 5\}/);
});

test("stacked series colours do not immediately repeat after the base token palette", () => {
  assert.match(source, /export function seriesColorVar\(index: number\) \{/);
  assert.match(source, /color-mix\(in srgb, \$\{base\} \$\{share\}%, \$\{mix\}\)/);
  assert.match(source, /fill=\{seriesColorVar\(s\)\}/);
  assert.doesNotMatch(source, /fill=\{SERIES_VARS\[s % SERIES_VARS\.length\]\}/);
});
