import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

for (const column of Array.from({ length: 21 }, (_, index) => index + 1)) {
  assert.match(
    css,
    new RegExp(`\\.herd-signals-page table\\.herd-signals-table th:nth-child\\(${column}\\),\\.herd-signals-page table\\.herd-signals-table td:nth-child\\(${column}\\)\\{[^}]*min-width:`),
    `herd signals live table column ${column} must have a desktop width guard`,
  );
}

assert.match(
  css,
  /\.herd-signals-page table\.herd-signals-table\{min-width:2860px\}/,
  "herd signals live table must scroll inside its wrapper instead of compressing 21 desktop columns",
);

assert.match(
  css,
  /@media\(max-width:860px\)\{[\s\S]*\.herd-signals-page table\.resp tbody tr\{[^}]*display:grid[^}]*border-bottom:1px solid[^}]*\}/,
  "herd signals mobile rows are divider-separated field grids inside the card, not bordered cards in a card",
);

assert.match(
  css,
  /@media\(max-width:860px\)\{[\s\S]*\.herd-signals-page table\.resp td\{[^}]*white-space:normal[^}]*\}/,
  "herd signals mobile table card values must wrap instead of causing page-level overflow",
);

assert.match(
  css,
  /@media\(max-width:760px\)\{[\s\S]*\.herd-signals-page aside\.drawer\{[^}]*width:100vw[^}]*max-width:100vw[^}]*height:100vh[^}]*\}/,
  "herd signals tag drawer must use the full mobile viewport width",
);

assert.match(
  css,
  /@media\(max-width:760px\)\{[\s\S]*\.herd-signals-page aside\.drawer \.patrow\{[^}]*display:grid[^}]*grid-template-columns:1fr 1fr[^}]*\}/,
  "herd signals drawer control row must wrap into a mobile grid",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.fsbd\{[^}]*overflow-x:hidden[^}]*\}/,
  "herd signals fullscreen history must prevent mobile page-wide horizontal spill",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.daterow\{[^}]*display:grid[^}]*grid-template-columns:1fr[^}]*\}/,
  "herd signals fullscreen range and overlay rows must stack on mobile",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.fs \.hchart\{[^}]*height:170px[^}]*\}/,
  "herd signals fullscreen chart must use a phone-sized height",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.fsel\{[^}]*min-width:0[^}]*max-width:100%[^}]*width:100%[^}]*\}/,
  "herd signals mobile filter dropdown shells must clamp to the viewport",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.fsel select\{[^}]*width:100%[^}]*min-width:0[^}]*max-width:100%[^}]*text-overflow:ellipsis[^}]*\}/,
  "herd signals mobile dropdown selected values must ellipsize instead of widening the page",
);

assert.match(
  css,
  /@media\(max-width:640px\)\{[\s\S]*\.herd-signals-page \.daterow input\[type=datetime-local\]\{[^}]*width:100%[^}]*min-width:0[^}]*max-width:100%[^}]*\}/,
  "herd signals mobile date inputs must fit inside the stacked range controls",
);
