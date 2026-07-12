// Public entrypoint for the process-integrity command lenses. App pages import from
// "@/features/process-integrity" (not deep submodule paths) per the import-boundary guardrail.
//
// Control Tower, Action Center, Protocol Adherence, and Workflows are TOP-LEVEL command screens
// (/, /action-center, /protocol-adherence, /workflows, /workflows/{row_id}) — NOT tabs or routes nested
// under Preventive Care (PC) / Vaccination, Parks, Procurement, or any vertical. Vaccination is the current DATA SCOPE that
// filters their content, not the UI hierarchy. These screens read the canonical process-integrity
// contracts (Action Center / Adherence / Workflow / Control Tower).
export { VaccinationActionCenterPage } from "./action-center";
export { ProtocolAdherencePage } from "./protocol-adherence";
export { VaccinationWorkflowsPage } from "./workflows-landing";
export { VaccinationWorkflowDrilldownPage } from "./workflow-drilldown";

// Shared process-integrity presentation model. Owned here (the command-room model); Preventive Care (PC) / Vaccination
// operations reuses WORK_STATE_META etc. via this barrel rather than a deep submodule path.
export * from "./process-integrity";
