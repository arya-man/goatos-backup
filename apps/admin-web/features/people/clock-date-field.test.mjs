import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: clock-date-field (J2 P2-11). The /people Clock toolbar date is the template date field
// (FormDateField: floating label, DD/MM/YYYY, 200px like its neighbours), not a mini summary button.
const src = readFileSync(new URL("./clock-screen.tsx", import.meta.url), "utf8");

test("guard: clock-date-field", () => {
  assert.doesNotMatch(src, /ThemedDatePicker/);
  assert.match(src, /width: \{ xs: 1, md: 200 \} \}\}>\s*<FormDateField\s+name="date"/);
});
