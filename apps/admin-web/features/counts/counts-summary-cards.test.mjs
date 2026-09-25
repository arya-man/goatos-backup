import assert from "node:assert/strict";
import test from "node:test";

import { buildCountsSummaryCards, countsSexDetail } from "./counts-summary-cards.ts";

const labels = { female: "Female", male: "Male", other: "Other" };
const summaryLabels = {
  fattening: "Fattening",
  bucks: "Bucks",
  breeding: "Breeding stock",
  icu: "ICU",
  k0: "K0",
  k1: "K1",
  k2: "K2",
  k3: "K3",
  k4: "K4",
};

test("requested Counts Breakdown cards roll up raw stage keys and sex splits", () => {
  const cards = buildCountsSummaryCards(
    [
      { key: "F2-Male", label: "Fattening male", count: 474, female: 0, male: 474, other: 0 },
      { key: "F2-Female", label: "Fattening female", count: 180, female: 180, male: 0, other: 0 },
      { key: "Buck", label: "Buck", count: 38, female: 0, male: 38, other: 0 },
      { key: "Mother", label: "Mother", count: 5, female: 5, male: 0, other: 0 },
      { key: "Milking", label: "Milking", count: 7, female: 7, male: 0, other: 0 },
      { key: "M0", label: "M0", count: 11, female: 11, male: 0, other: 0 },
      { key: "Warmup", label: "Warmup", count: 13, female: 13, male: 0, other: 0 },
      { key: "Pregnant", label: "Pregnant", count: 17, female: 17, male: 0, other: 0 },
      { key: "Non-Pregnant", label: "Non-Pregnant", count: 803, female: 803, male: 0, other: 0 },
      { key: "ICU-Kid", label: "ICU-Kid", count: 19, female: 11, male: 8, other: 0 },
      { key: "ICU-Non-Pregnant", label: "ICU-Non-Pregnant", count: 3, female: 3, male: 0, other: 0 },
      { key: "ICU-Buck", label: "ICU-Buck", count: 2, female: 0, male: 2, other: 0 },
      { key: "ICU-Pregnant", label: "ICU-Pregnant", count: 5, female: 5, male: 0, other: 0 },
      { key: "ICU-F2-Male", label: "ICU-F2-Male", count: 7, female: 0, male: 7, other: 0 },
      { key: "ICU-F1-Male", label: "ICU-F1-Male", count: 11, female: 0, male: 11, other: 0 },
      { key: "F2-ICU", label: "F2-ICU", count: 13, female: 0, male: 13, other: 0 },
      { key: "K0", label: "K0", count: 2, female: 1, male: 1, other: 0 },
      { key: "K1", label: "K1", count: 4, female: 2, male: 2, other: 0 },
      { key: "K2", label: "K2", count: 12, female: 4, male: 8, other: 0 },
      { key: "K3", label: "K3", count: 45, female: 32, male: 13, other: 0 },
      { key: "K4", label: "K4", count: 6, female: 3, male: 3, other: 0 },
    ],
    labels,
    summaryLabels,
  );
  const byKey = new Map(cards.map((card) => [card.key, card]));

  assert.equal(byKey.get("fattening")?.count, 654);
  assert.equal(byKey.get("fattening")?.detail, "180 female · 474 male");
  assert.equal(byKey.get("bucks")?.count, 38);
  assert.equal(byKey.get("breeding")?.count, 856);
  assert.equal(byKey.get("icu")?.label, "ICU");
  assert.equal(byKey.get("icu")?.count, 60);
  assert.equal(byKey.get("k0")?.count, 2);
  assert.equal(byKey.get("k1")?.count, 4);
  assert.equal(byKey.get("k2")?.count, 12);
  assert.equal(byKey.get("k3")?.count, 45);
  assert.equal(byKey.get("k4")?.count, 6);
});

test("summary classification prefers raw stage key over display label", () => {
  const cards = buildCountsSummaryCards(
    [
      { key: "F2-Male", label: "Changed presentation label", count: 9, female: 0, male: 9, other: 0 },
      { key: "Mystery", label: "Fattening male", count: 99, female: 0, male: 99, other: 0 },
    ],
    labels,
    summaryLabels,
  );
  const fattening = cards.find((card) => card.key === "fattening");

  assert.equal(fattening?.count, 9);
  assert.equal(fattening?.detail, "0 female · 9 male");
});

test("sex detail includes other only when it contributes to the count", () => {
  assert.equal(
    countsSexDetail({ female: 12, male: 8, other: 2 }, labels),
    "12 female · 8 male · 2 other",
  );
  assert.equal(
    countsSexDetail({ female: 12, male: 8, other: 0 }, labels),
    "12 female · 8 male",
  );
});

test("a stage no card names gets its own Other stages card, so the cards add up to the herd", () => {
  const points = [
    { key: "K1", label: "K1", count: 4, female: 2, male: 2, other: 0 },
    { key: "Weaner", label: "Weaner", count: 7, female: 3, male: 4, other: 0 },
  ];
  const cards = buildCountsSummaryCards(points, labels, { ...summaryLabels, other: "Other stages" });
  const other = cards.find((card) => card.key === "other_stages");
  assert.equal(other?.count, 7);
  assert.equal(other?.detail, "3 female · 4 male");
  assert.equal(cards.reduce((sum, card) => sum + card.count, 0), 11);
  const none = buildCountsSummaryCards(points.slice(0, 1), labels, { ...summaryLabels, other: "Other stages" });
  assert.equal(none.find((card) => card.key === "other_stages"), undefined);
});
