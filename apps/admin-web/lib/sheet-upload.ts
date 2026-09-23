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
  format: "csv" | "xlsx",
): string {
  return `${HEALTH_REGISTER_SHEETS}/${encodeURIComponent(animalClass)}/${action}?format=${format}`;
}

export function registerSheetImportUrl(animalClass: string): string {
  return `${HEALTH_REGISTER_SHEETS}/${encodeURIComponent(animalClass)}/import`;
}
