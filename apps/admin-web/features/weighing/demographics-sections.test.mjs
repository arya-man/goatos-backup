import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { demographicsSectionsForTab } from "./demographics-sections.ts";

const here = dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");

test("General reads the composition section, so the pens table's Breed column has data", () => {
  // Regression (pr294 sweep F1): the General tab dropped this read and every Breed cell read
  // "No data available".
  assert.equal(demographicsSectionsForTab("general"), "composition");
});

test("every other tab keeps its own section and the load/fcr tabs read none", () => {
  assert.equal(demographicsSectionsForTab("breed"), "dimensions");
  assert.equal(demographicsSectionsForTab("birth"), "origin");
  assert.equal(demographicsSectionsForTab("shed"), "shed_type");
  assert.equal(demographicsSectionsForTab("weight"), "weight_bands");
  assert.equal(demographicsSectionsForTab("time"), "weekly_gain");
  assert.equal(demographicsSectionsForTab("load"), "");
  assert.equal(demographicsSectionsForTab("fcr"), "");
  assert.equal(demographicsSectionsForTab("toString"), "", "a prototype key is not a tab");
});

test("the page derives both whether and what to read from the one table", () => {
  assert.match(pageSource, /demographicsSections = demographicsSectionsForTab\(tab\)/);
  assert.match(pageSource, /wantsDemographics = demographicsSections !== ""/);
});
