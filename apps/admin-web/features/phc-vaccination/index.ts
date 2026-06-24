// Public entrypoint for the PHC / Vaccination module. App pages import from "@/features/phc-vaccination"
// (not deep submodule paths) per the import-boundary guardrail.
//
// PHC is the VERTICAL; Vaccination is the MODULE under it. This feature owns the vaccination OPERATIONS
// surface at /vaccination only — due/overdue drives, scheduled/in-progress sessions, proof + verification
// backlog, rejected/rework, and the park/shed EXECUTION section (scoped by the top-bar park dropdown).
// It LINKS OUT to the top-level command lenses (Control Tower / Action Center / Protocol Adherence /
// Workflows); it must never embed or recreate them. Park/shed execution data may be powered by the Parks
// read-model endpoints, but it renders inside /vaccination — Parks is not a separate vaccination product.
export { VaccinationOperationsPage } from "./operations";
export { VaccinationPassportSection } from "./passport-section";
