import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// The valuation's "Last saved" stamp arrives from the backend already farm-readable
// ("02/10/2026 21:47", IST). Feeding it back through new Date() reads it US-style -- 2 October
// became "10/02/2026" -- whenever the day is 12 or less, so the page renders it verbatim.
test("valuation Last saved is rendered verbatim, never re-parsed as a date", () => {
  const src = readFileSync(new URL("./valuation-section.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(src, /fmtDate(Time)?\(\s*v\.updated_at\s*\)/);
  assert.doesNotMatch(src, /new Date\(\s*v\.updated_at\s*\)/);
  assert.match(src, /\{copy\(pageContract, "valuation\.updated"\)\} \{v\.updated_at\}/);
});
