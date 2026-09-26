/**
 * The Weights pages' Sex filter rules, shared by /weighing/weights and /weighing/analytics so the
 * two cannot disagree about which kids a URL means. Pure: the page-contract reads live in
 * sex-filter-contract.ts, so these rules are testable without the app.
 *
 * The choices are the farm's genders from Configuration > Items & settings (the `weights_sexes`
 * group, compiled by the backend from that list). Until the 2026-09-26 audit they were male and
 * female typed into each page, so a gender the farm added never reached Weighing.
 *
 * MALE stays the default (maintainer request 2026-09-01): an absent `sex` means male, `sex=all`
 * means every kid, and a value that is not one of the farm's genders falls back to the default
 * rather than emptying the page -- a hand-edited URL must not take the screen down.
 */
export const SEX_ALL = "all";
const SEX_DEFAULT = "male";

export type SexChoice = { value: string; label: string };

/** The filter the reads take: "" for every kid, otherwise a gender code the farm has configured. */
export function resolveSexFilter(raw: string | undefined, choices: readonly SexChoice[]): string {
  if (raw === SEX_ALL) return "";
  if (raw && choices.some((choice) => choice.value === raw)) return raw;
  return SEX_DEFAULT;
}

/** The shape every gender code has (the backend's animalvocab.ValidCodeShape). */
const SEX_CODE_SHAPE = /^[a-z][a-z0-9_]{0,39}$/;

/**
 * resolveSexFilter for a place that has no page contract to read the farm's genders from (the
 * landing redirect, the lump-markers route): any well-shaped gender code is passed on, and the
 * backend answers for it -- a code nobody carries simply matches no kid.
 */
export function sexFilterFromUrl(raw: string | undefined): string {
  if (raw === SEX_ALL) return "";
  if (raw && SEX_CODE_SHAPE.test(raw)) return raw;
  return SEX_DEFAULT;
}

/** What the control shows: the reads take "" for every kid, the control carries its own All. */
export function sexControlValue(sexFilter: string): string {
  return sexFilter === "" ? SEX_ALL : sexFilter;
}

/** The farm's word for a gender code, or the code itself when the list does not carry it. */
export function sexLabel(code: string, choices: readonly SexChoice[]): string {
  return choices.find((choice) => choice.value === code)?.label ?? code;
}

/**
 * Which copy key names the kids the page counted, and what replaces `{sex}` in it. Every kid, male
 * and female have sentences of their own; any other gender reads the `other` sentence with `{sex}`
 * replaced by that gender's name, lower-cased as it sits mid-sentence ("castrated male kids").
 */
export function sexSentenceKey(
  sexFilter: string,
  keys: { all: string; perSex: (sex: string) => string },
  choices: readonly SexChoice[],
): { key: string; sex: string } {
  if (sexFilter === "") return { key: keys.all, sex: "" };
  if (sexFilter === "male" || sexFilter === "female") return { key: keys.perSex(sexFilter), sex: "" };
  return { key: keys.perSex("other"), sex: sexLabel(sexFilter, choices).toLowerCase() };
}
