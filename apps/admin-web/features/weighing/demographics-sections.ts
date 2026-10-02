// Which section of the weight-demographics read each /weighing/analytics tab asks for.
//
// One table, so the landing tab cannot lose its read again by merge drift: General reads the
// `composition` section ONLY, for the pens table's Breed column (maintainer request 2026-09-21).
// The pen's resident cohort lives on the same `shed_composition` chips the Weights table renders,
// so the two screens can never name a different breed for one pen, and the landing tab pays for
// that one section rather than the dimensions, bands and weekly gain it draws nothing from.
// A tab absent from the table makes no demographics read at all.
const DEMOGRAPHICS_SECTIONS: Readonly<Record<string, string>> = {
  general: "composition",
  breed: "dimensions",
  birth: "origin",
  shed: "shed_type",
  weight: "weight_bands",
  time: "weekly_gain",
};

/** The demographics `sections` value a tab reads, or "" when the tab reads no demographics. */
export function demographicsSectionsForTab(tab: string): string {
  return Object.prototype.hasOwnProperty.call(DEMOGRAPHICS_SECTIONS, tab) ? DEMOGRAPHICS_SECTIONS[tab] : "";
}
