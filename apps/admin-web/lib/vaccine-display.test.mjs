import assert from "node:assert/strict";
import test from "node:test";

import { vaccinationDriveDisplayName } from "./vaccine-display.ts";

test("vaccinationDriveDisplayName strips Preventive Care matrix wrappers from natural ET+TT labels", () => {
  assert.equal(
    vaccinationDriveDisplayName("Preventive Care Vaccination Matrix ET+TT adult course dose 2: 120 due by 2026-07-25"),
    "ET+TT adult course dose 2",
  );
});

