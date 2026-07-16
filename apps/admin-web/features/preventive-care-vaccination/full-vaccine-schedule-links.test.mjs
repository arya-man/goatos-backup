import assert from "node:assert/strict";
import test from "node:test";

import { vaccinationScheduleCellAsOf, vaccinationScheduleCohortAsOf } from "./full-vaccine-schedule-links.ts";

test("full schedule detail links use the clicked protocol cell date", () => {
  assert.equal(
    vaccinationScheduleCellAsOf(
      {
        nextDue: "2026-12-29T00:00:00+05:30",
        lastDose: "2026-06-30T00:00:00+05:30",
      },
      2026,
    ),
    "2026-12-29",
  );
});

test("full schedule row links fall back to a dated protocol cell", () => {
  assert.equal(
    vaccinationScheduleCohortAsOf(
      {
        cells: [
          { nextDue: "2027-01-06T00:00:00+05:30" },
          { lastDose: "2026-07-18T00:00:00+05:30" },
        ],
      },
      2026,
    ),
    "2026-07-18",
  );
});

test("full schedule links do not carry an unrelated year date", () => {
  assert.equal(vaccinationScheduleCellAsOf({ nextDue: "2027-01-06T00:00:00+05:30" }, 2026), undefined);
});
