/**
 * Vaccine taxonomy constants shared with the backend protocol/app/publish.go validation.
 * These must match the server's valid sets exactly.
 *
 * Backend source of truth: backend/internal/protocol/app/publish.go
 *   - validVaccineTypes: live, killed, toxoid (+ matrix for wrapper)
 *   - validPathogenClasses: viral, bacterial
 *   - validCourseTypes: single, booster
 */

// Valid vaccine types (live, killed, toxoid, matrix for wrapper)
// Individual vaccines use: live, killed, toxoid
// Wrapper vaccine (type=matrix, code=vaccination.matrix) uses: matrix
export const VALID_VACCINE_TYPES = ["live", "killed", "toxoid"];
export const VALID_VACCINE_TYPES_WITH_MATRIX = ["live", "killed", "toxoid", "matrix"];

// Valid pathogen classes (organism types)
export const VALID_PATHOGEN_CLASSES = ["viral", "bacterial"];

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
