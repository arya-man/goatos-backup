import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: phone-tap-width (TR1-#8). The 44px phone floor sets width AND height for chips, toggle buttons
// (grouped ones outrank the group's --size rule). The date-range clear is now the MUI X field clear
// button, an IconButton already in the floor.
test("chips and toggles get a 44px phone width", () => {
  const src = readFileSync(new URL("./phone-tap-styles.tsx", import.meta.url), "utf8");
  const block = src.slice(src.indexOf("'.MuiIconButton-root.MuiIconButton-root'"), src.indexOf("]: { minWidth: TAP, minHeight: TAP }"));
  for (const sel of [".MuiChip-root.MuiChip-root", ".MuiToggleButtonGroup-root .MuiToggleButton-root.MuiToggleButton-root"]) {
    assert.ok(block.includes(`'${sel}'`), `${sel} is in the width+height floor`);
  }
});
