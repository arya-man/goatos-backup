/**
 * Vaccine taxonomy constants shared with the backend protocol/app/publish.go validation.
 * These must match the server's valid sets exactly.
 *
 * BACKEND SOURCE OF TRUTH: backend/internal/protocol/app/publish.go
 *   - validVaccineTypes: live, killed, toxoid, combo, unknown_review_needed (+ matrix for wrapper)
 *   - validPathogenClasses: viral, bacterial, mixed, unknown_review_needed
 *   - validCourseTypes: single, booster
 *
 * These lists deliberately mirror the backend Config option groups
 * (adminui service.pageOptionGroups vaccine_types / vaccine_pathogen_classes / vaccine_course_types)
 * and the V1 Implementation Contract in docs/preventive-care-vaccination/vaccination-rules.md, which
 * require the reviewed "combo" type and "mixed" / "unknown_review_needed" values. R2-07b: they were
 * previously a narrower second list (live/killed/toxoid, viral/bacterial), so the UI both offered
 * options its own Save validation rejected AND diverged from what the server accepts. A backend guard
 * test (protocol IsValidVaccineType/IsValidPathogenClass) keeps the option groups and publish
 * validation from drifting; keep these constants in lockstep with that reconciled set.
 */

// Valid vaccine types. Individual vaccines: live, killed, toxoid, combo (reviewed), unknown_review_needed.
// Wrapper vaccine (type=matrix, code=vaccination.matrix) additionally uses: matrix.
export const VALID_VACCINE_TYPES = ["live", "killed", "toxoid", "combo", "unknown_review_needed"];
export const VALID_VACCINE_TYPES_WITH_MATRIX = ["live", "killed", "toxoid", "combo", "unknown_review_needed", "matrix"];

// Valid pathogen classes (organism types), including reviewed combination/unknown values.
export const VALID_PATHOGEN_CLASSES = ["viral", "bacterial", "mixed", "unknown_review_needed"];

// Valid course types (immunological course classification)
export const VALID_COURSE_TYPES = ["single", "booster"];

/**
 * getValidVaccineTypes returns the set of valid vaccine types for a given context.
 * @param isWrapper - if true, returns types valid for the matrix wrapper (includes "matrix")
 * @returns array of valid vaccine type strings
 */
export function getValidVaccineTypes(isWrapper: boolean): string[] {
  return isWrapper ? VALID_VACCINE_TYPES_WITH_MATRIX : VALID_VACCINE_TYPES;
}
