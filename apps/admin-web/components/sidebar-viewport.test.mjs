import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

assert.match(
  css,
  /\.layout\{[^}]*height:calc\(100dvh - 58px\)[^}]*min-height:0[^}]*overflow:hidden[^}]*\}/,
  "admin shell layout must own the viewport below the top bar so long pages scroll inside .main",
);

assert.match(
  css,
  /\.side\{[^}]*align-self:stretch[^}]*height:100%[^}]*min-height:0[^}]*overflow:auto[^}]*\}/,
  "sidebar must stretch to the shell viewport and keep its own scroll area",
);

assert.match(
  css,
  /\.main\{[^}]*min-height:0[^}]*overflow:auto[^}]*\}/,
  "main content must be the scroll container when a page is taller than the sidebar",
);

assert.match(
  css,
  /@media\(max-width:860px\)\{[\s\S]*\.side\{[^}]*position:fixed[^}]*top:58px[^}]*height:calc\(100dvh - 58px\)[^}]*\}/,
  "mobile off-canvas sidebar must fill the viewport below the top bar",
);
