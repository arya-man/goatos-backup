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

// PR #294 K10: a native <select> under the phone border-box rule kept the template's 1.4375em height,
// leaving 12px of content box for a 24px line ("Coimbatore" cut in half on the routines drawer).
import { test as k10 } from "node:test";
k10("a native select sizes to its line on a phone", async () => {
  const { readFileSync: rf } = await import("node:fs");
  const { match } = (await import("node:assert/strict")).default;
  match(rf(new URL("./phone-tap-styles.tsx", import.meta.url), "utf8"), /'\.MuiInputBase-root select\.MuiInputBase-input\.MuiInputBase-input': \{ height: 'auto' \}/);
});
