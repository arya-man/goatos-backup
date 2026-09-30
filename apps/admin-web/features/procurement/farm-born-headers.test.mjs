import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: farm-born-headers-one-line (J2 P2-5). /sales/farm-born breakdown headers wrapped onto 2-3
// lines in half-width cards and cut "Earn…". Headers hold one line; the cards are full width below xl.
const page = readFileSync(new URL("./sales-farm-born.tsx", import.meta.url), "utf8");
const layout = readFileSync(new URL("./sales-layout.ts", import.meta.url), "utf8");

test("guard: farm-born-headers-one-line", () => {
  assert.match(page, /"& th\.MuiTableCell-alignRight": \{ whiteSpace: "nowrap" \}/);
  assert.doesNotMatch(page, /maxWidth: "12ch"/);
  assert.match(layout, /bornHalf: \{ xs: 12, xl: 6 \}/);
});
