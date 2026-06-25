// Public entrypoint for the Operations Audit surface. App pages import from "@/features/operations-audit"
// (not deep submodule paths) per the import-boundary convention.
//
// One cross-operations audit read surface (operator/admin/system) — not a per-pillar mini screen.
// Data comes from the generated admin client; empty states are real backend-empty states, not fixtures.
export { OperationsAuditPage } from "./audit-log";
