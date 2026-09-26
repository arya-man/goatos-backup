/**
 * The farm word for an FCR cohort value. Shared by the FCR tab (server) and its pen table (client), so
 * it lives in a plain module: a function exported from a "use client" file cannot be called while the
 * server renders. The backend sends breed in the register's own spelling, sex as the register's
 * lower-case key, and "mixed" / "unknown" as sentinels; none of those may reach the screen raw.
 */
export type CohortWords = {
  mixedBreed: string;
  mixedSex: string;
  unknown: string;
  male: string;
  female: string;
  /** Every gender the farm configured, code -> name, so a third gender never reaches the screen raw. */
  sexes?: Readonly<Record<string, string>>;
};

/**
 * `kind` says which fact the value is, because "mixed" means a different thing for each: a pen of
 * several breeds, or a pen of both sexes. Rendering both as "Mixed" put "Mixed · Mixed" on a chip.
 */
export function cohortWord(value: string, labels: CohortWords, kind: "breed" | "sex"): string {
  if (value === "mixed") return kind === "breed" ? labels.mixedBreed : labels.mixedSex;
  if (value === "unknown" || value === "") return labels.unknown;
  if (value === "male") return labels.male;
  if (value === "female") return labels.female;
  if (kind === "sex" && labels.sexes?.[value]) return labels.sexes[value];
  return value;
}
