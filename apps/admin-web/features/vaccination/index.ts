// Public entrypoint for the vaccination feature. App pages must import from "@/features/vaccination"
// (not deep submodule paths) per the import-boundary guardrail. Same-feature internal imports use
// relative paths.
export { VaccinationActionCenterPage } from "./action-center";
export { ProtocolAdherencePage } from "./adherence";
export { VaccinationTabs } from "./vaccination-tabs";
export { VaccinationPassportSection } from "./passport-section";
