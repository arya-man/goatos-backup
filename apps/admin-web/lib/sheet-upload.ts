// The diagnosis-register sheet endpoints and the header a write carries.
//
// They live in lib rather than beside the component because they are PROTOCOL, not copy: the
// contract-literal guard reads a capitalised string inside a feature component as text a person
// might see, and it is right to -- every visible word on these screens comes from the backend's
// page contract. A header name is neither.
export const HEALTH_REGISTER_SHEETS = "/api/admin/health-register-sheets";
export const IDEMPOTENCY_HEADER = "Idempotency-Key";

export function registerSheetHref(
  animalClass: string,
  action: "template" | "export",
  format: "csv" | "xlsx" | "json",
): string {
  return `${HEALTH_REGISTER_SHEETS}/${encodeURIComponent(animalClass)}/${action}?format=${format}`;
}

export function registerSheetImportUrl(animalClass: string): string {
  return `${HEALTH_REGISTER_SHEETS}/${encodeURIComponent(animalClass)}/import`;
}

/**
 * The blank template is the SAME FILE for every type: the backend builds it from the column
 * header and one worked example of each row kind, and never reads the animal class. The route
 * still carries a class segment, so this names the one it passes rather than leaving a reader to
 * wonder why the adult template is offered beside a kid register.
 */
export const TEMPLATE_CLASS_PLACEHOLDER = "adult";
