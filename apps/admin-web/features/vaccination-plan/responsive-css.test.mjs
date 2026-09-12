import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

assert.match(
  css,
  /\.vplan \{[^}]*width: min\(100%, 1180px\)[^}]*margin: 0 auto[^}]*min-width: 0[^}]*\}/,
  "vaccination plan page must fit the available app column instead of carrying a desktop-only width",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vplan \.head-top \{[^}]*grid-template-columns: minmax\(0, 1fr\)[^}]*\}/,
  "vaccination plan mobile header must collapse to one column",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vplan \.vlist \{[^}]*flex-direction: row[^}]*overflow-x: auto[^}]*\}/,
  "vaccination plan editor vaccine rail must become a horizontal selector on mobile",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vplan \.ab-in \{[^}]*display: grid[^}]*grid-template-columns: 1fr 1fr[^}]*\}/,
  "vaccination plan editor action bar must not force a desktop flex row on mobile",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vplan \.field input, \.vplan \.field select, \.vplan \.field textarea,[\s\S]*\.vp-nvf input, \.vp-nvf select \{[^}]*min-width: 0[^}]*max-width: 100%[^}]*\}/,
  "vaccination plan mobile form controls and dropdown values must stay inside the viewport",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vplan \.pop \{[^}]*position: fixed[^}]*left: 16px[^}]*right: 16px[^}]*bottom: 16px[^}]*width: auto[^}]*\}/,
  "vaccination plan duration dropdown popover must be viewport-clamped on mobile",
);

assert.match(
  css,
  /@media \(max-width: 640px\) \{[\s\S]*\.vp-seg, \.vplan \.seg \{[^}]*display: grid[^}]*grid-template-columns: 1fr[^}]*width: 100%[^}]*\}/,
  "vaccination plan segmented dropdown-like choices must stack as full-width mobile controls",
);
