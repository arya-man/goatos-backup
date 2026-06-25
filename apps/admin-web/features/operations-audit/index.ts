// Public entrypoint for the Operations Audit surface. App pages import from "@/features/operations-audit"
// (not deep submodule paths) per the import-boundary convention.
//
// One cross-operations audit read surface (operator/admin/system) — not a per-pillar mini screen. Built
// mock-faithful but data-empty until the listOperationsAudit / getOperationsAuditSummary generated clients
// are published.
export { OperationsAuditPage } from "./audit-log";
