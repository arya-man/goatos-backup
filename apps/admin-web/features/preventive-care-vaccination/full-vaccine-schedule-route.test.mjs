import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(
  new URL("./full-vaccine-schedule.tsx", import.meta.url),
  "utf8",
);

test("vaccination full schedule interactions stay inside the vaccination IA", () => {
  assert.equal(
    source.includes("/calendar/drive/"),
    false,
    "Vaccination schedule cells must not deep-link to Calendar drive detail; use the vaccination-owned drawer/route.",
  );
  assert.match(source, /href=\{shedDrawerHref\(row\)\}/);
});
