import test from "node:test";
import assert from "node:assert/strict";

import { salePriceForAnimal, valueHeadMix } from "./sale-price.ts";

const row = (species, management_stage, sex, price) => ({
  species,
  management_stage,
  sex,
  price_per_kg_inr: price,
  effective_from: "2026-09-24",
  set_by: "",
});

const prices = [
  row("goat", "", "", 425),
  row("sheep", "", "", 400),
  row("goat", "K3", "male", 500),
  row("goat", "K3", "female", 460),
  row("sheep", "K3", "male", 380),
];

// Maintainer decision 2026-09-24 -- the same cases backend
// growthdirector/domain.TestPenPriceValuesEachAnimalAtItsStageAndSex pins, so the Load-wise tab and
// the FCR tab price one animal at one figure.
test("an animal takes its stage x sex price, else its species default", () => {
  assert.equal(salePriceForAnimal(prices, "goat", "K3", "male"), 500);
  assert.equal(salePriceForAnimal(prices, "goat", "K3", "female"), 460);
  assert.equal(salePriceForAnimal(prices, "goat", "F2", "male"), 425, "no override: goat default");
  assert.equal(salePriceForAnimal(prices, "sheep", "K3", "female"), 400, "overrides are per species");
  assert.equal(salePriceForAnimal(prices, "Sheep", "k3", "MALE"), 380, "matching ignores case");
  assert.equal(salePriceForAnimal(prices, "goat", "", "male"), 425, "no stage on the register: default");
  assert.equal(salePriceForAnimal(prices, "cattle", "K3", "male"), null, "an unpriced species has no price");
});

test("a head mix is valued animal by animal, and not at all when any animal is unpriced", () => {
  const mix = [
    { species: "goat", management_stage: "K3", sex: "male", animals: 10 },
    { species: "goat", management_stage: "F2", sex: "female", animals: 5 },
  ];
  assert.equal(valueHeadMix(prices, mix, 20), 10 * 20 * 500 + 5 * 20 * 425);
  assert.equal(valueHeadMix(prices, [], 20), 0);
  assert.equal(
    valueHeadMix(prices, [...mix, { species: "cattle", management_stage: "", sex: "", animals: 1 }], 20),
    null,
  );
});
