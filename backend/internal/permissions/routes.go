package permissions

import "strings"

type Route struct {
	OperationID string
	Method      string
	Pattern     string
	// Permissions is ANDed: the caller must hold EVERY listed permission.
	Permissions []string
	// AnyPermissions is ORed: the caller must hold AT LEAST ONE listed permission.
	// Use it for an either/or surface (e.g. a read a planner OR a monitor may open)
	// where naming both in Permissions would deny anyone holding only one.
	AnyPermissions []string
	AdminOnly      bool
}

var protectedRoutes = []Route{
	{OperationID: "searchGoats", Method: "GET", Pattern: "/goats/search", Permissions: []string{GoatRead}},
	{OperationID: "getGoatPassport", Method: "GET", Pattern: "/goats/{goat_id}", Permissions: []string{GoatRead}},
	{OperationID: "getGoatTimeline", Method: "GET", Pattern: "/goats/{goat_id}/timeline", Permissions: []string{GoatRead}},
	{OperationID: "getHerdRegisterSummary", Method: "GET", Pattern: "/herd-register/summary", Permissions: []string{CountsRead}},
	{OperationID: "getCountsBreakdown", Method: "GET", Pattern: "/counts/breakdown", Permissions: []string{CountsRead}},
	{OperationID: "getMilkPreparation", Method: "GET", Pattern: "/counts/milk-preparation", Permissions: []string{CountsRead}},
	{OperationID: "getAppCountsMilkPreparation", Method: "GET", Pattern: "/app/counts/milk-preparation", Permissions: []string{CountsWrite}},
	{OperationID: "resolveIdentifier", Method: "GET", Pattern: "/identifiers/{type}/{value}/resolve", Permissions: []string{GoatRead}},
	{OperationID: "addGoatIdentifier", Method: "POST", Pattern: "/admin/goats/{goat_id}/identifiers", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "retireGoatIdentifier", Method: "POST", Pattern: "/admin/goats/{goat_id}/identifiers/{identifier_id}/retire", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "createAdminGoat", Method: "POST", Pattern: "/admin/goats", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "previewAdminGoatBulkImport", Method: "POST", Pattern: "/admin/goats/bulk-preview", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "commitAdminGoatBulkImport", Method: "POST", Pattern: "/admin/goats/bulk-commit", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "moveGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/move", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "exitGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/exit", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "criticalDeathExitGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/critical-death-exit", Permissions: []string{GoatWriteHealth}},
	{OperationID: "stageGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/stage", Permissions: []string{GoatWriteIdentity}},
	// Whole-pen cohort reclassification. CEO-only, and the PREVIEW is gated identically to the
	// commit on purpose: it reports a pen's live composition, which is not something a principal
	// who may not perform the action needs to enumerate.
	{OperationID: "previewReclassifyShedStage", Method: "POST", Pattern: "/admin/goats/shed-stage/preview", Permissions: []string{GoatReclassifyShedStage}},
	{OperationID: "commitReclassifyShedStage", Method: "POST", Pattern: "/admin/goats/shed-stage/commit", Permissions: []string{GoatReclassifyShedStage}},
	{OperationID: "healthGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/health", Permissions: []string{GoatWriteHealth}},
	{OperationID: "reproductiveGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/reproductive", Permissions: []string{GoatWriteHealth}},
	{OperationID: "identityGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/identity", Permissions: []string{GoatWriteIdentity}},
	// Bulk status-update kernel spans the reproductive/health/exit axes, so it
	// requires both the identity and health write grants (superuser data op).
	{OperationID: "previewBulkStatusUpdate", Method: "POST", Pattern: "/admin/goats/bulk-status/preview", Permissions: []string{GoatWriteIdentity, GoatWriteHealth}},
	{OperationID: "commitBulkStatusUpdate", Method: "POST", Pattern: "/admin/goats/bulk-status/commit", Permissions: []string{GoatWriteIdentity, GoatWriteHealth}},
	{OperationID: "listOperationsAudit", Method: "GET", Pattern: "/operations/audit", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "getOperationsAuditSummary", Method: "GET", Pattern: "/operations/audit/summary", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "getOperationsKernelHealth", Method: "GET", Pattern: "/operations/kernel-health", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "listOutboxDLQ", Method: "GET", Pattern: "/operations/dlq", Permissions: []string{OperatorsViewAudit}},
	// CEO/CxO read-only leadership assistant. Route-level defense-in-depth uses
	// the leadership dashboard-bootstrap grant; the ceoai orchestrator itself is
	// the authoritative gate (it requires RoleCEOInternal and derives tenant +
	// role ONLY from the session, never from the request body).
	{OperationID: "askCeoAssistant", Method: "POST", Pattern: "/ceo-ai/ask", Permissions: []string{AdminWebBootstrap}},
	// Leadership assistant thread and starters surface. Route-level defense-in-depth
	// uses the same leadership dashboard-bootstrap grant; the ConversationHandler is
	// the authoritative gate (requires RoleCEOInternal and scopes every read/write to
	// the session tenant+actor). The starters route doubles as the client
	// leadership-capability probe (200 => show launcher).
	{OperationID: "getCeoAssistantStarters", Method: "GET", Pattern: "/ceo-ai/starters", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "listCeoAssistantConversations", Method: "GET", Pattern: "/ceo-ai/conversations", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "createCeoAssistantConversation", Method: "POST", Pattern: "/ceo-ai/conversations", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "listCeoAssistantMessages", Method: "GET", Pattern: "/ceo-ai/conversations/{id}/messages", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "renameCeoAssistantConversation", Method: "PATCH", Pattern: "/ceo-ai/conversations/{id}", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "deleteCeoAssistantConversation", Method: "DELETE", Pattern: "/ceo-ai/conversations/{id}", Permissions: []string{AdminWebBootstrap}},
	// Admin-only internal step-trace debug surface. Route-level defense-in-depth
	// uses the leadership dashboard-bootstrap grant; the AdminTraceHandler is the
	// authoritative gate (it requires RoleCEOInternal and derives tenant scope
	// ONLY from the session, returning an identical 403 for non-admins and a
	// cross-tenant miss so request-id existence can't be probed).
	{OperationID: "getCeoAssistantTrace", Method: "GET", Pattern: "/ceo-ai/admin/trace/{request_id}", Permissions: []string{AdminWebBootstrap}, AdminOnly: true},
	{OperationID: "replayOutboxDLQ", Method: "POST", Pattern: "/operations/dlq/replay", Permissions: []string{OperationsRepair}},
	{OperationID: "discardOutboxDLQ", Method: "POST", Pattern: "/operations/dlq/discard", Permissions: []string{OperationsRepair}},

	{OperationID: "listLocations", Method: "GET", Pattern: "/admin/locations", Permissions: []string{LocationsRead}},
	{OperationID: "createLocation", Method: "POST", Pattern: "/admin/locations", Permissions: []string{LocationsWrite}},
	{OperationID: "getLocation", Method: "GET", Pattern: "/admin/locations/{location_id}", Permissions: []string{LocationsRead}},
	{OperationID: "updateLocation", Method: "PATCH", Pattern: "/admin/locations/{location_id}", Permissions: []string{LocationsWrite}},
	{OperationID: "deleteLocation", Method: "DELETE", Pattern: "/admin/locations/{location_id}", Permissions: []string{LocationsRetire}},
	{OperationID: "retireLocation", Method: "POST", Pattern: "/admin/locations/{location_id}/retire", Permissions: []string{LocationsRetire}},
	{OperationID: "listLocationChildren", Method: "GET", Pattern: "/admin/locations/{location_id}/children", Permissions: []string{LocationsRead}},
	{OperationID: "getLocationUsage", Method: "GET", Pattern: "/admin/locations/{location_id}/usage", Permissions: []string{LocationsRead}},
	{OperationID: "listLocationAliases", Method: "GET", Pattern: "/admin/locations/{location_id}/aliases", Permissions: []string{LocationsRead}},
	{OperationID: "createLocationAlias", Method: "POST", Pattern: "/admin/locations/{location_id}/aliases", Permissions: []string{LocationsWrite}},
	{OperationID: "updateLocationAlias", Method: "PATCH", Pattern: "/admin/locations/{location_id}/aliases/{alias_id}", Permissions: []string{LocationsWrite}},
	{OperationID: "deleteLocationAlias", Method: "DELETE", Pattern: "/admin/locations/{location_id}/aliases/{alias_id}", Permissions: []string{LocationsRetire}},
	{OperationID: "retireLocationAlias", Method: "POST", Pattern: "/admin/locations/{location_id}/aliases/{alias_id}/retire", Permissions: []string{LocationsRetire}},
	{OperationID: "listLocationCapacity", Method: "GET", Pattern: "/admin/locations/{location_id}/capacity", Permissions: []string{LocationsRead}},
	{OperationID: "createLocationCapacity", Method: "POST", Pattern: "/admin/locations/{location_id}/capacity", Permissions: []string{LocationsWrite}},
	{OperationID: "updateLocationCapacity", Method: "PATCH", Pattern: "/admin/locations/{location_id}/capacity/{capacity_record_id}", Permissions: []string{LocationsWrite}},
	{OperationID: "deleteLocationCapacity", Method: "DELETE", Pattern: "/admin/locations/{location_id}/capacity/{capacity_record_id}", Permissions: []string{LocationsRetire}},
	{OperationID: "listLocationReviewItems", Method: "GET", Pattern: "/admin/location-review-items", Permissions: []string{LocationsReview}},
	{OperationID: "createLocationReviewItem", Method: "POST", Pattern: "/admin/location-review-items", Permissions: []string{LocationsReview}},
	{OperationID: "resolveLocationReviewItem", Method: "POST", Pattern: "/admin/location-review-items/{review_id}/resolve", Permissions: []string{LocationsReview}},

	{OperationID: "listOperators", Method: "GET", Pattern: "/admin/operators", Permissions: []string{OperatorsRead}},
	{OperationID: "createOperator", Method: "POST", Pattern: "/admin/operators", Permissions: []string{OperatorsWrite}},
	{OperationID: "getOperator", Method: "GET", Pattern: "/admin/operators/{operator_id}", Permissions: []string{OperatorsRead}},
	{OperationID: "updateOperator", Method: "PATCH", Pattern: "/admin/operators/{operator_id}", Permissions: []string{OperatorsWrite}},
	{OperationID: "activateOperator", Method: "POST", Pattern: "/admin/operators/{operator_id}/activate", Permissions: []string{OperatorsActivate}},
	{OperationID: "deactivateOperator", Method: "POST", Pattern: "/admin/operators/{operator_id}/deactivate", Permissions: []string{OperatorsDeactivate}},
	{OperationID: "listOperatorGrants", Method: "GET", Pattern: "/admin/operators/{operator_id}/grants", Permissions: []string{OperatorsRead}},
	{OperationID: "createOperatorGrant", Method: "POST", Pattern: "/admin/operators/{operator_id}/grants", Permissions: []string{OperatorsWrite}},
	{OperationID: "assignOperatorCapability", Method: "POST", Pattern: "/admin/operators/{operator_id}/capabilities", Permissions: []string{OperatorsManageCapability}},
	{OperationID: "removeOperatorCapability", Method: "DELETE", Pattern: "/admin/operators/{operator_id}/capabilities/{capability_id}", Permissions: []string{OperatorsManageCapability}},
	{OperationID: "listOperatorDevices", Method: "GET", Pattern: "/admin/operators/{operator_id}/devices", Permissions: []string{OperatorsManageDevice}},
	{OperationID: "revokeOperatorDevice", Method: "POST", Pattern: "/admin/operators/{operator_id}/devices/{device_id}/revoke", Permissions: []string{OperatorsManageDevice}},

	{OperationID: "appMe", Method: "GET", Pattern: "/app/me", Permissions: []string{AppBootstrap}},
	{OperationID: "appBootstrap", Method: "GET", Pattern: "/app/bootstrap", Permissions: []string{AppBootstrap}},
	{OperationID: "adminWebBootstrap", Method: "GET", Pattern: "/admin-web/bootstrap", Permissions: []string{AdminWebBootstrap}},
	{OperationID: "registerAppDevice", Method: "POST", Pattern: "/app/devices/register", Permissions: []string{AppBootstrap}},
	{OperationID: "heartbeatAppDevice", Method: "POST", Pattern: "/app/devices/{device_id}/heartbeat", Permissions: []string{AppBootstrap}},
	{OperationID: "deregisterAppDevice", Method: "POST", Pattern: "/app/devices/{device_id}/deregister", Permissions: []string{AppBootstrap}},
	// Mobile live remote-config poll (docs/mobile/backend-driven-config.md): ETag/revision +
	// cache_policy, presentation feature flags/owned-module registry, and bounded client runtime
	// knobs. Same AppBootstrap "any authenticated app principal" gate as /app/bootstrap.
	{OperationID: "appConfig", Method: "GET", Pattern: "/app/config", Permissions: []string{AppBootstrap}},
	// Mobile telemetry ingest: every authenticated app principal may report client-side state
	// transitions/failures. The event payload carries module/device context; feature authority stays
	// on the underlying action routes, not on this observability write.
	{OperationID: "recordAppAnalyticsEvent", Method: "POST", Pattern: "/app/analytics/events", Permissions: []string{AppBootstrap}},

	{OperationID: "listSOPs", Method: "GET", Pattern: "/admin/sops", Permissions: []string{SOPRead}},
	{OperationID: "createSOP", Method: "POST", Pattern: "/admin/sops", Permissions: []string{SOPWrite}},
	{OperationID: "getSOP", Method: "GET", Pattern: "/admin/sops/{sop_id}", Permissions: []string{SOPRead}},
	{OperationID: "createSOPVersion", Method: "POST", Pattern: "/admin/sops/{sop_id}/versions", Permissions: []string{SOPWrite}},
	{OperationID: "getSOPVersion", Method: "GET", Pattern: "/admin/sops/{sop_id}/versions/{sop_version_id}", Permissions: []string{SOPRead}},
	{OperationID: "dryRunSOPVersion", Method: "POST", Pattern: "/admin/sops/{sop_id}/versions/{sop_version_id}/dry-run", Permissions: []string{SOPRead}},
	{OperationID: "publishSOPVersion", Method: "POST", Pattern: "/admin/sops/{sop_id}/versions/{sop_version_id}/publish", Permissions: []string{SOPPublish}},
	{OperationID: "retireSOPVersion", Method: "POST", Pattern: "/admin/sops/{sop_id}/versions/{sop_version_id}/retire", Permissions: []string{SOPPublish}},
	{OperationID: "listTasks", Method: "GET", Pattern: "/admin/tasks", Permissions: []string{TaskRead}},
	{OperationID: "createTask", Method: "POST", Pattern: "/admin/tasks", Permissions: []string{TaskAssign}},
	{OperationID: "getTask", Method: "GET", Pattern: "/admin/tasks/{task_id}", Permissions: []string{TaskRead}},
	{OperationID: "assignTask", Method: "POST", Pattern: "/admin/tasks/{task_id}/assign", Permissions: []string{TaskAssign}},
	{OperationID: "verifyTask", Method: "POST", Pattern: "/admin/tasks/{task_id}/verify", Permissions: []string{TaskVerify}},
	{OperationID: "reworkTask", Method: "POST", Pattern: "/admin/tasks/{task_id}/rework", Permissions: []string{TaskVerify}},
	{OperationID: "listAgedFailedSOPSubmissionFanouts", Method: "GET", Pattern: "/admin/tasks/submission-fanouts/failed", Permissions: []string{TaskVerify}},
	{OperationID: "retrySOPReviewFanouts", Method: "POST", Pattern: "/admin/tasks/review-fanouts/retry", Permissions: []string{TaskVerify}},
	{OperationID: "retrySOPSubmissionFanouts", Method: "POST", Pattern: "/admin/tasks/submission-fanouts/retry", Permissions: []string{TaskVerify}},
	{OperationID: "listAppTasks", Method: "GET", Pattern: "/app/tasks", Permissions: []string{TaskRead}},
	{OperationID: "getAppTask", Method: "GET", Pattern: "/app/tasks/{task_id}", Permissions: []string{TaskRead}},
	{OperationID: "getAppSOPVersion", Method: "GET", Pattern: "/app/sop-versions/{sop_version_id}", Permissions: []string{TaskRead}},
	// EITHER/OR, not task.execute alone. Capturing proof is not a separate act from doing the
	// work: every write that MANDATES a video is unsubmittable until the proof handshake
	// completes. Gating these four on task.execute alone silently broke every executor that
	// holds a MODULE execute grant instead of the operator's general one -- a Growth Director
	// holds weighing.execute and is authorized for appRecordWeighingShedObservation, but
	// POST /app/proofs/uploads answered 403 permission_denied on every attempt, so no proof
	// ever reached SYNCED and lump-sum Submit stayed permanently disabled (phone-QA 2026-08-03).
	//
	// Widening growth_director to task.execute would have been the wrong lever: task.execute is
	// the operator's GENERAL task-execution grant and carries vaccination task execution with
	// it, which this role must not have. So the ROUTE becomes either/or and the role keeps
	// exactly the one module it owns.
	// health.execute is the same shape and needs the same lever. completeAppHealthWorkItem is
	// gated on HealthExecute and its completion mandates evidence, so a holder that is NOT also a
	// general operator (ceo_internal holds HealthExecute without TaskExecute) could record the
	// treatment session and never upload its proof -- the identical permanently-unsubmittable
	// state weighing hit above. Adding health.execute here keeps each role scoped to the one
	// module it owns; handing it task.execute instead would carry vaccination SOP submission with
	// it, which is the privilege escalation this route shape exists to avoid.
	{OperationID: "createProofUpload", Method: "POST", Pattern: "/app/proofs/uploads", AnyPermissions: []string{TaskExecute, WeighingExecute, HealthExecute, FeedDirectionComplete}},
	{OperationID: "uploadProofLocal", Method: "PUT", Pattern: "/app/proofs/{proof_id}/upload", AnyPermissions: []string{TaskExecute, WeighingExecute, HealthExecute, FeedDirectionComplete}},
	{OperationID: "completeProofUpload", Method: "POST", Pattern: "/app/proofs/{proof_id}/complete", AnyPermissions: []string{TaskExecute, WeighingExecute, HealthExecute, FeedDirectionComplete}},
	{OperationID: "deleteUnattachedProofUpload", Method: "DELETE", Pattern: "/app/proofs/{proof_id}", AnyPermissions: []string{TaskExecute, WeighingExecute, HealthExecute, FeedDirectionComplete}},
	{OperationID: "downloadProof", Method: "GET", Pattern: "/app/proofs/{proof_id}/download", Permissions: []string{TaskRead}},
	{OperationID: "recordAppAnalyticsEvent", Method: "POST", Pattern: "/app/analytics/events", Permissions: []string{AppBootstrap}},
	{OperationID: "recordAppScanCapture", Method: "POST", Pattern: "/app/tasks/{task_id}/scan-captures", Permissions: []string{TaskExecute}},
	{OperationID: "recordAppScanAttempt", Method: "POST", Pattern: "/app/tasks/{task_id}/scan-attempts", Permissions: []string{TaskExecute}},
	{OperationID: "submitAppTask", Method: "POST", Pattern: "/app/tasks/{task_id}/submissions", Permissions: []string{TaskExecute}},
	{OperationID: "getShedCompletionSummary", Method: "GET", Pattern: "/app/tasks/{task_id}/shed-completion-summary", Permissions: []string{TaskRead}},

	// Procurement/source-entry backend slice.
	{OperationID: "listProcurementSourceEntryLoads", Method: "GET", Pattern: "/procurement/source-entry/loads", Permissions: []string{ProcurementRead}},
	{OperationID: "createProcurementSourceEntryLoad", Method: "POST", Pattern: "/procurement/source-entry/loads", Permissions: []string{ProcurementWrite}},
	{OperationID: "getProcurementSourceEntryLoad", Method: "GET", Pattern: "/procurement/source-entry/loads/{load_id}", Permissions: []string{ProcurementRead}},
	{OperationID: "addProcurementSourceEntryLoadGoat", Method: "POST", Pattern: "/procurement/source-entry/loads/{load_id}/goats", Permissions: []string{ProcurementWrite}},
	{OperationID: "recordProcurementHFVaccinationEvidence", Method: "POST", Pattern: "/procurement/source-entry/goats/{goat_id}/hf-vaccination-evidence", Permissions: []string{ProcurementWrite}},
	{OperationID: "reviewProcurementHFVaccinationEvidence", Method: "POST", Pattern: "/procurement/source-entry/hf-vaccination-evidence/{evidence_id}/review", Permissions: []string{ProcurementReview}},
	{OperationID: "recordProcurementSourceHealth", Method: "POST", Pattern: "/procurement/source-entry/goats/{goat_id}/source-health", Permissions: []string{ProcurementWrite}},
	{OperationID: "recordProcurementPreDispatchDecision", Method: "POST", Pattern: "/procurement/source-entry/goats/{goat_id}/pre-dispatch-decision", Permissions: []string{ProcurementReview}},
	{OperationID: "dispatchProcurementSourceEntryLoad", Method: "POST", Pattern: "/procurement/source-entry/loads/{load_id}/dispatch", Permissions: []string{ProcurementWrite}},
	{OperationID: "recordProcurementArrivalReview", Method: "POST", Pattern: "/procurement/source-entry/loads/{load_id}/arrival-review", Permissions: []string{ProcurementReview}},
	{OperationID: "acceptProcurementIntake", Method: "POST", Pattern: "/procurement/source-entry/loads/{load_id}/accept-intake", Permissions: []string{ProcurementReview}},

	// Procurement VENDOR REGISTER (/procurement/vendors), the counterparty contact book.
	//
	// Gated on the dedicated VendorRead/VendorWrite rather than ProcurementRead/ProcurementWrite:
	// those are held by seven roles including operator and park_head, and the register carries
	// negotiated prices, phone numbers and banking instruments. See VendorRead's doc comment.
	//
	// The catalog route is the business-managed dropdown vocabulary behind the register's selects
	// (record types, breeds, states, cities, statuses, feed kinds). It is a READ of the same screen
	// and carries VendorRead.
	{OperationID: "listProcurementVendors", Method: "GET", Pattern: "/procurement/vendors", Permissions: []string{VendorRead}},
	{OperationID: "getProcurementVendor", Method: "GET", Pattern: "/procurement/vendors/{vendor_id}", Permissions: []string{VendorRead}},
	{OperationID: "createProcurementVendor", Method: "POST", Pattern: "/procurement/vendors", Permissions: []string{VendorWrite}},
	{OperationID: "updateProcurementVendor", Method: "PUT", Pattern: "/procurement/vendors/{vendor_id}", Permissions: []string{VendorWrite}},
	{OperationID: "updateProcurementVendorStatus", Method: "POST", Pattern: "/procurement/vendors/{vendor_id}/status", Permissions: []string{VendorWrite}},
	{OperationID: "listProcurementVendorCatalog", Method: "GET", Pattern: "/procurement/vendor-catalog", Permissions: []string{VendorRead}},
	// Procurement command-lens data is served by the TOP-LEVEL command screens via ?domain=procurement,
	// not nested /procurement/source-entry/* routes. Those nested lens routes are intentionally not registered.

	// Phase 1A — protocol config / vaccination obligation engine.
	{OperationID: "listProtocolConfigs", Method: "GET", Pattern: "/protocols", Permissions: []string{ProtocolRead}},
	{OperationID: "listAnimalStages", Method: "GET", Pattern: "/protocols/animal-stages", Permissions: []string{ProtocolRead}},
	{OperationID: "createProtocolDefinition", Method: "POST", Pattern: "/protocols", Permissions: []string{ProtocolWrite}},
	{OperationID: "createProtocolVersion", Method: "POST", Pattern: "/protocols/{protocol_id}/versions", Permissions: []string{ProtocolWrite}},
	{OperationID: "addProtocolRule", Method: "POST", Pattern: "/protocols/versions/{version_id}/rules", Permissions: []string{ProtocolWrite}},
	{OperationID: "getProtocolVersion", Method: "GET", Pattern: "/protocols/versions/{version_id}", Permissions: []string{ProtocolRead}},
	{OperationID: "publishProtocolVersion", Method: "POST", Pattern: "/protocols/versions/{version_id}/publish", Permissions: []string{ProtocolPublish}},
	{OperationID: "vaccinationImpactPreview", Method: "POST", Pattern: "/protocols/vaccination/impact-preview", Permissions: []string{ProtocolRead}},
	{OperationID: "runVaccinationManualCampaign", Method: "POST", Pattern: "/vaccination/manual-campaigns", Permissions: []string{VaccinationCampaign}},
	{OperationID: "listActionCenterObligations", Method: "GET", Pattern: "/action-center/obligations", Permissions: []string{ObligationRead}},
	{OperationID: "listVaccinationActionCenter", Method: "GET", Pattern: "/vaccination/action-center", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "countVaccinationActionCenter", Method: "GET", Pattern: "/vaccination/action-center/counts", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationProtocolAdherence", Method: "GET", Pattern: "/vaccination/adherence", Permissions: []string{ObligationRead, VaccinationRead}},
	// This route backs the app-tier vaccination Alerts tab (AlertsViewModel ->
	// ControlTowerRepository -> GET /control-tower/vaccination). AnyPermissions,
	// not Permissions: RoleOperator holds neither ObligationRead nor
	// VaccinationRead (the leadership oversight pair every OTHER vaccination
	// admin route ANDs), so ANDing them here 403'd every operator's Alerts tab
	// and the mobile client rendered that 403 as an honest-looking empty state
	// (bug found on-device 2026-08-04). VaccinationAlertsRead is a route-scoped
	// capability granted ONLY to RoleOperator for THIS route -- see its doc
	// comment in permissions.go for why the fix did not just add
	// ObligationRead/VaccinationRead to the operator's base grant set. Every
	// other caller already holds ObligationRead AND VaccinationRead, so this
	// OR-list is a superset of the previous AND-list for them: no regression.
	{OperationID: "getVaccinationControlTower", Method: "GET", Pattern: "/control-tower/vaccination", AnyPermissions: []string{ObligationRead, VaccinationRead, VaccinationAlertsRead}},
	{OperationID: "getWorkflowDrilldown", Method: "GET", Pattern: "/workflows/{row_id}", Permissions: []string{ObligationRead}},
	{OperationID: "getVaccinationWorkflowDrilldown", Method: "GET", Pattern: "/vaccination/workflows/{row_id}", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationOperations", Method: "GET", Pattern: "/vaccination/operations", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationSchedule", Method: "GET", Pattern: "/vaccination/schedule", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "upsertVaccinationDriveDateOverride", Method: "POST", Pattern: "/vaccination/schedule/drive-date-overrides", Permissions: []string{VaccinationCampaign}},
	{OperationID: "listVaccinationDriveAssignments", Method: "GET", Pattern: "/vaccination/drive-assignments", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "listVaccinationExecution", Method: "GET", Pattern: "/vaccination/execution", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationExecutionShedDrilldown", Method: "GET", Pattern: "/vaccination/execution/sheds/{shed_id}", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationCommandBoard", Method: "GET", Pattern: "/vaccination/command", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	// Live drive-day tracker: same park-scope authority and same permission triple as its command-board
	// sibling — it is the same vaccination execution data at administration grain, read live.
	{OperationID: "getVaccinationLiveTracker", Method: "GET", Pattern: "/vaccination/live-tracker", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "listVaccinationShedSummary", Method: "GET", Pattern: "/vaccination/sheds", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationShedDetail", Method: "GET", Pattern: "/vaccination/sheds/{shed_id}", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationShedAnimals", Method: "GET", Pattern: "/vaccination/sheds/{shed_id}/animals", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationCapacityConfig", Method: "GET", Pattern: "/vaccination/capacity-config", Permissions: []string{ProtocolRead}},
	{OperationID: "putVaccinationCapacityConfig", Method: "PUT", Pattern: "/vaccination/capacity-config", Permissions: []string{VaccinationCampaign}},
	// Phase 1 CONFIG-ONLY: operator shift + N-active-operators-per-day default assignment config.
	// Not yet consumed by the drive scheduler (Phase 5). Same config-authority permission as capacity.
	{OperationID: "getVaccinationOperatorAssignmentConfig", Method: "GET", Pattern: "/vaccination/operator-assignment/config", Permissions: []string{ProtocolRead}},
	{OperationID: "putVaccinationOperatorAssignmentConfig", Method: "PUT", Pattern: "/vaccination/operator-assignment/config", Permissions: []string{VaccinationCampaign}},
	{OperationID: "listWeighingCampaigns", Method: "GET", Pattern: "/weighing/campaigns", Permissions: []string{WeighingMonitor}},
	{OperationID: "createWeighingCampaign", Method: "POST", Pattern: "/weighing/campaigns", Permissions: []string{WeighingPlan}},
	{OperationID: "updateWeighingCampaign", Method: "PUT", Pattern: "/weighing/campaigns/{campaign_id}", Permissions: []string{WeighingPlan}},
	{OperationID: "publishWeighingCampaign", Method: "POST", Pattern: "/weighing/campaigns/{campaign_id}/publish", Permissions: []string{WeighingPlan}},
	{OperationID: "exportWeighingCampaignCSV", Method: "GET", Pattern: "/weighing/campaigns/{campaign_id}/export", Permissions: []string{WeighingMonitor}},
	{OperationID: "exportWeighingCsv", Method: "GET", Pattern: "/weighing/export.csv", Permissions: []string{WeighingMonitor}},
	// EITHER/OR, not both. These two are READS, and weighing/app/service.go's
	// canPlanOrMonitor deliberately admits WeighingPlan OR WeighingMonitor (it calls
	// RolesAuthorize twice for exactly that reason). Route.Permissions is ANDed, so
	// naming both there contradicted the service and would 403 a monitor-only actor at
	// the middleware before the service's own either/or gate ever ran. AnyPermissions is
	// the OR form; the fine-grained WRITE checks stay in the service.
	// These two feed the CREATE WIZARD's park picker and its per-park shed page, so they are
	// planning surfaces despite being reads: WeighingPlan only. A monitor sees the same parks
	// and sheds through the task list and the Operators surface, which carry their own gates.
	{OperationID: "appWeighingPlannerCatalog", Method: "GET", Pattern: "/app/weighing/planner/catalog", Permissions: []string{WeighingPlan}},
	// Per-park bucket page of the same planner read; same gate as the park picker it drills from.
	{OperationID: "appWeighingPlannerParkBuckets", Method: "GET", Pattern: "/app/weighing/planner/parks/{park_id}/buckets", Permissions: []string{WeighingPlan}},
	{OperationID: "appListWeighingCampaigns", Method: "GET", Pattern: "/app/weighing/campaigns", Permissions: []string{AppBootstrap}},
	// Single-task read behind a notification deep link. AppBootstrap for the same reason the
	// bucket page below is: the SERVICE applies the assignee/planner split AND resolves the
	// task's own park before answering, so a tighter route permission here would lock an
	// assignee out of the task they were sent to work on.
	{OperationID: "appGetWeighingCampaign", Method: "GET", Pattern: "/app/weighing/campaigns/{campaign_id}", Permissions: []string{AppBootstrap}},
	// Park chips for the weighing oversight surfaces. AnyPermissions (ORed), not Permissions
	// (ANDed): the planner catalog is the only other park list and it is WeighingPlan, which is
	// CEO-only, so a Growth Director -- who holds monitor and oversee_operators and never plan --
	// had no park vocabulary they could legitimately read. Naming all three in Permissions would
	// 403 every one of them at the middleware, since no role holds the whole set.
	{OperationID: "appListWeighingParks", Method: "GET", Pattern: "/app/weighing/parks", AnyPermissions: []string{WeighingMonitor, WeighingPlan, WeighingOverseeOperators}},
	// Task-detail bucket page. Gated on AppBootstrap like the task list it drills
	// from — the SERVICE narrows an execute-only caller to their own buckets, so a
	// tighter route permission here would lock assignees out of their own task.
	{OperationID: "appListWeighingCampaignSheds", Method: "GET", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds", Permissions: []string{AppBootstrap}},
	{OperationID: "appGetWeighingScopeRoster", Method: "GET", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster", Permissions: []string{WeighingExecute}},
	{OperationID: "appGetWeighingLeadershipShedVideos", Method: "GET", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/videos", Permissions: []string{WeighingMonitor}},
	{OperationID: "appListWeighingLeadershipSheds", Method: "GET", Pattern: "/app/weighing/leadership/sheds", Permissions: []string{WeighingMonitor}},
	// CEO-tier weighing history: weight observations per RFID across weigh days, park-scoped.
	{OperationID: "getWeighingWeightHistory", Method: "GET", Pattern: "/app/weighing/weight-history", Permissions: []string{WeighingMonitor}},
	// CEO-tier ADG / growth aggregate: herd-level Average Daily Gain read model, park-scoped.
	{OperationID: "getWeighingLeadershipGrowthADG", Method: "GET", Pattern: "/app/weighing/leadership/growth", Permissions: []string{WeighingMonitor}},
	{OperationID: "getWeighingShedWeights", Method: "GET", Pattern: "/app/weighing/shed-weights", Permissions: []string{WeighingMonitor}},
	{OperationID: "adminGetWeighingShedWeights", Method: "GET", Pattern: "/weighing/shed-weights", Permissions: []string{WeighingMonitor}},
	{OperationID: "adminGetWeighingWeightDemographics", Method: "GET", Pattern: "/weighing/weight-demographics", Permissions: []string{WeighingMonitor}},
	{OperationID: "adminGetWeighingLeadershipGrowth", Method: "GET", Pattern: "/weighing/leadership/growth", Permissions: []string{WeighingMonitor}},
	{OperationID: "appRecordWeighingAnimalObservation", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/animal-observations", Permissions: []string{WeighingExecute}},
	{OperationID: "appRecordWeighingShedObservation", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/shed-observations", Permissions: []string{WeighingExecute}},
	{OperationID: "appSubmitWeighingScope", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/submit", Permissions: []string{WeighingExecute}},
	{OperationID: "appReopenWeighingScope", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen", Permissions: []string{WeighingMonitor}},
	// Explicit close is the terminal twin of reopen and carries the same
	// monitor-only authority: only leadership may end weighing work that will
	// never finish, and closing may strand not-accepted buckets.
	{OperationID: "appCloseWeighingScope", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/close", Permissions: []string{WeighingMonitor}},
	{OperationID: "appCloseWeighingCampaign", Method: "POST", Pattern: "/app/weighing/campaigns/{campaign_id}/close", Permissions: []string{WeighingMonitor}},
	// PHASE 2 Calendar / Control Tower weighing process state (read-only).
	{OperationID: "getWeighingProcessState", Method: "GET", Pattern: "/weighing/process-state", Permissions: []string{WeighingMonitor}},
	// The weighing module's OWN lifecycle alerts feed. AnyPermissions, never
	// Permissions: everyone with a weighing job needs it but nobody holds all
	// three capabilities -- an operator holds execute, the CEO holds plan+monitor
	// but not execute, and the Growth Director intentionally holds BOTH execute
	// and monitor. ANDing them would deny every real seat.
	//
	// Gated on WEIGHING capabilities ONLY. It must never require
	// ObligationRead/VaccinationRead: /alerts is the vaccination process-integrity
	// feed, and pointing the weighing bar at it is exactly what made the previous
	// weighing alerts tab 403 for weighing operators and got it deleted.
	// VerificationReview belongs here. The weighing verification consumer addresses
	// "weighing.proof.pending.verifier" notifications to the VERIFIER by workforce_member_id, and
	// the verifier holds none of the weighing capabilities -- so the feed 403'd and their Alerts
	// tab was permanently empty while 11 messages sat addressed to them (observed 2026-08-08).
	// A person the system chooses as a recipient must be able to read their own inbox. The feed
	// itself is already scoped to context->>'member_id' = caller, so this grants no one sight of
	// anybody else's alerts.
	{OperationID: "appListWeighingAlerts", Method: "GET", Pattern: "/app/weighing/alerts", AnyPermissions: []string{WeighingExecute, WeighingMonitor, WeighingPlan, VerificationReview}},
	// Growth Director widgets on the admin-web Weights screen. Its OWN module
	// (weighing is herd-isolated; these widgets need breed/sex + the feed sheet),
	// but the SAME gate as the Weights page reads it renders under.
	{OperationID: "adminGetGrowthDirectorWeights", Method: "GET", Pattern: "/growth-director/weights", Permissions: []string{WeighingMonitor}},
	// App-tier vaccination execution: gated on AppBootstrap = any authenticated
	// app user (operators + leadership all hold it), NOT the admin-tier
	// LocationsRead/ObligationRead/VaccinationRead/CalendarAction combo RoleOperator
	// lacks. Mirrors the /app/roster/* precedent (commit 6c8962be) -- these are the
	// core operator app actions (scan the shed roster, reschedule an obligation),
	// so gating them on admin-tier perms 403s every field operator. The admin
	// /vaccination/* routes above stay on their existing admin-tier perms.
	// Vaccination's OWN alerts feed, the twin of appListWeighingAlerts. It carries
	// VaccinationAlertsRead -- the SAME route-scoped permission the control-tower
	// feed uses -- so the operator keeps the access the 2026-08-04 fix gave them
	// and no role gains anything new. The feed's audience is enforced inside the
	// query by member_id equality, so this permission opens the tab, not the data.
	{OperationID: "appListVaccinationAlerts", Method: "GET", Pattern: "/app/vaccination/alerts", AnyPermissions: []string{VaccinationAlertsRead, ObligationRead, VaccinationRead}},
	// The feed module's OWN alerts feed, the twin of appListWeighingAlerts /
	// appListVaccinationAlerts. FeedDirectionRead admits feed_director, park_head, and
	// RoleCEOInternal (all three already hold it); VerificationReview admits the verifier, who
	// holds no feed_direction.* capability. See notificationbridge.pendingModuleProfiles["feed"]
	// for the recipient set this feed reads back (verifier, park_head, feed_director, ceo).
	{OperationID: "appListFeedAlerts", Method: "GET", Pattern: "/app/feed/alerts", AnyPermissions: []string{FeedDirectionRead, VerificationReview}},
	// The counts module's OWN alerts feed, the twin of appListWeighingAlerts /
	// appListVaccinationAlerts. See the CountsAlertsRead doc comment (permissions.go) for why this
	// route needs its own dedicated permission rather than CountsRead/CountsWrite: health_director
	// is Counts' documented owner and verification-push recipient but deliberately holds neither,
	// because COUNTS IS AN OFF FEATURE and granting either would switch it on. CountsWrite admits
	// park_head and CEO; VerificationReview admits the verifier.
	{OperationID: "appListCountsAlerts", Method: "GET", Pattern: "/app/counts/alerts", AnyPermissions: []string{CountsAlertsRead, CountsWrite, VerificationReview}},
	{OperationID: "appListVaccinationExecution", Method: "GET", Pattern: "/app/vaccination/execution", Permissions: []string{AppBootstrap}},
	{OperationID: "appGetVaccinationExecutionShedDrilldown", Method: "GET", Pattern: "/app/vaccination/execution/sheds/{shed_id}", Permissions: []string{AppBootstrap}},
	{OperationID: "appScanRoster", Method: "GET", Pattern: "/app/vaccination/execution/sheds/{shed_id}/roster", Permissions: []string{AppBootstrap}},
	{OperationID: "appTaskOptionValues", Method: "GET", Pattern: "/app/vaccination/tasks/{task_id}/option-values", Permissions: []string{AppBootstrap}},
	{OperationID: "appRescheduleObligation", Method: "POST", Pattern: "/app/vaccination/obligations/{obligation_id}/reschedule", Permissions: []string{AppBootstrap}},
	// App-tier data-gaps + coverage-rollup overlays: same AppBootstrap "any authenticated app
	// principal" gate as appScanRoster/appRescheduleObligation above (mobile Data gaps + Doses
	// given overlays, Overlays.kt DataGapsSheet/DosesGivenSheet).
	{OperationID: "appVaccinationGaps", Method: "GET", Pattern: "/app/vaccination/gaps", Permissions: []string{AppBootstrap}},
	{OperationID: "appVaccinationCoverage", Method: "GET", Pattern: "/app/vaccination/coverage", Permissions: []string{AppBootstrap}},
	// Raising a sick-goat report is HealthReport, not HealthDiagnose: the field operator who
	// spots the animal opens the case, and the configured course/treatment authority stays with
	// the PC Director tier (maintainer decision 2026-07-30). Route.Permissions is ANDed, so this
	// must name the single permission every raiser holds.
	{OperationID: "openAppHealthCase", Method: "POST", Pattern: "/app/health/cases", Permissions: []string{HealthReport}},
	{OperationID: "listAppHealthWorkItems", Method: "GET", Pattern: "/app/health/work-items", Permissions: []string{HealthRead}},
	{OperationID: "getAppHealthWorkItem", Method: "GET", Pattern: "/app/health/work-items/{health_session_id}", Permissions: []string{HealthRead}},
	{OperationID: "completeAppHealthWorkItem", Method: "POST", Pattern: "/app/health/work-items/{health_session_id}/complete", Permissions: []string{HealthExecute}},
	// Authored treatment protocols (/health-config/*), the surface behind the Health Config screen.
	//
	// The read/write split is the whole point: a principal may be allowed to INSPECT the standing
	// dosages without being allowed to change them. Note that opening a draft is a WRITE
	// (/health-config/drafts creates a draft row when none is open), so it carries the write
	// permission despite reading like a read.
	{OperationID: "listHealthConfigProtocols", Method: "GET", Pattern: "/health-config/protocols", Permissions: []string{HealthConfigRead}},
	{OperationID: "getHealthConfigProtocol", Method: "GET", Pattern: "/health-config/protocols/{protocol_version_id}", Permissions: []string{HealthConfigRead}},
	{OperationID: "createHealthConfigDisease", Method: "POST", Pattern: "/health-config/diseases", Permissions: []string{HealthConfigWrite}},
	{OperationID: "openHealthConfigDraft", Method: "POST", Pattern: "/health-config/drafts", Permissions: []string{HealthConfigWrite}},
	{OperationID: "saveHealthConfigDraft", Method: "POST", Pattern: "/health-config/drafts/save", Permissions: []string{HealthConfigWrite}},
	{OperationID: "publishHealthConfigDraft", Method: "POST", Pattern: "/health-config/protocols/{protocol_version_id}/publish", Permissions: []string{HealthConfigWrite}},
	{OperationID: "discardHealthConfigDraft", Method: "POST", Pattern: "/health-config/protocols/{protocol_version_id}/discard", Permissions: []string{HealthConfigWrite}},
	{OperationID: "getFeedDirectionGenerationPreview", Method: "GET", Pattern: "/feed-direction/generation-preview", Permissions: []string{FeedDirectionRead}},
	{OperationID: "listFeedDirectionCountsProjectionExceptions", Method: "GET", Pattern: "/feed-direction/counts-projection/exceptions", Permissions: []string{FeedDirectionRead}},
	{OperationID: "resolveFeedDirectionCountsProjectionException", Method: "POST", Pattern: "/feed-direction/counts-projection/exceptions/{exception_id}/resolve", Permissions: []string{FeedDirectionOversee}},
	{OperationID: "dismissFeedDirectionCountsProjectionException", Method: "POST", Pattern: "/feed-direction/counts-projection/exceptions/{exception_id}/dismiss", Permissions: []string{FeedDirectionOversee}},
	// Feed-direction GENERATION (backend/internal/feeddirection): projected shed counts + the
	// authored ration grid -> per-session feed quantities.
	//
	// The preview gates on FeedDirectionRead, the feed-direction read surface's own permission.
	// It used to reuse ProtocolRead -- the VACCINATION protocol read -- which handed every feed
	// reader the vaccination surface and made the Feed Director inexpressible. See
	// FeedDirectionRead.
	//
	// The packing worklist gets its OWN permission instead. It is a different top-level surface read
	// by a different audience -- the store team that physically weighs and bags -- and it is the
	// natural candidate for widening to RoleOperator once capture is built on it. Folding it into
	// ProtocolRead would make that widening impossible without also handing the packing team the
	// vaccination protocol surface. See FeedPackingRead.
	//
	// Both are GET-only. This module has no write path: it generates what SHOULD be fed, while
	// recording what WAS fed belongs to backend/internal/feed. No route here needs an
	// Idempotency-Key because no route here has a side effect to replay.
	{OperationID: "getFeedDirectionPreview", Method: "GET", Pattern: "/feed-direction/preview", Permissions: []string{FeedDirectionRead}},
	{OperationID: "getFeedPackingWorklist", Method: "GET", Pattern: "/feed-packing/worklist", Permissions: []string{FeedPackingRead}},
	// The pre-gate instant completion (POST /feed-direction/complete) is intentionally absent: its
	// route is unregistered and its store unwired, because completing at operator submit bypasses the
	// verifier gate. Do not re-add it here.
	// The verifier-gated feed DISTRIBUTION and PACKING completions (maintainer decision, 2026-07-26).
	// Both are operator WRITE paths on the feed-direction surface and reuse the operator write twin
	// FeedDirectionComplete. They MUST be registered here: the auth middleware 403s
	// (route_not_registered) any route not in this table, so an unregistered write path is unreachable.
	{OperationID: "completeFeedDistributionSession", Method: "POST", Pattern: "/feed-direction/distribution/complete", Permissions: []string{FeedDirectionComplete}},
	{OperationID: "completeFeedPackingSession", Method: "POST", Pattern: "/feed-direction/packing/complete", Permissions: []string{FeedDirectionComplete}},
	// Daily feed transport. The LIST is a read and gates on its own read permission; only the
	// SUBMIT keeps the write twin (maintainer decision 2026-08-05). Both were FeedDirectionComplete,
	// which meant looking at the transport worklist required the authority to record that transport
	// happened -- and therefore hid the page from the Feed Director, who deliberately holds no
	// execute grant. See FeedTransportRead.
	{OperationID: "listFeedTransportTasks", Method: "GET", Pattern: "/feed-transport/tasks", Permissions: []string{FeedTransportRead}},
	{OperationID: "submitFeedTransportTask", Method: "POST", Pattern: "/feed-transport/tasks/{task_id}/submit", Permissions: []string{FeedDirectionComplete}},

	// Authored feed configuration (/feed-config/*), the surface behind the Feed Config screen.
	//
	// Kept separate from the /feed-direction/* routes directly above, and gated on its own
	// permissions rather than on ProtocolRead/ProtocolWrite. Direction is today's operational output;
	// this is the standing rule that produced it. A principal who may look at this morning's feed
	// sheet is not thereby entitled to read -- let alone rewrite -- the tenant-wide ration grid the
	// whole farm is fed from.
	//
	// The POSTs are the editable half the maintainer asked for. Each requires an
	// Idempotency-Key, is effective-dated (an edit closes the current row and opens a new one rather
	// than overwriting), and is recorded in feed_config_write_log. FeedConfigWrite does NOT imply
	// FeedConfigRead and vice versa: Route.Permissions is ANDed, so each route names exactly what it
	// needs and a read-only auditor stays read-only.
	//
	// createFeedConfigFeedItem is on FeedConfigWrite like the rest, even though adding a catalog
	// entry authors no quantity of its own. It is the same authority for the same reason the
	// experiment routes below share it: the feed-item catalog is the VOCABULARY every rate, factor
	// and experiment cell is keyed by, so whoever may add to it shapes the grid the whole farm is
	// fed from. A read-only auditor must not be able to extend it.
	{OperationID: "listFeedConfigRationRates", Method: "GET", Pattern: "/feed-config/ration-rates", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigRationGroups", Method: "GET", Pattern: "/feed-config/ration-groups", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigShedTags", Method: "GET", Pattern: "/feed-config/shed-tags", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigFeedItems", Method: "GET", Pattern: "/feed-config/feed-items", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigSessionTemplates", Method: "GET", Pattern: "/feed-config/session-templates", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigSchedule", Method: "GET", Pattern: "/feed-config/schedule", Permissions: []string{FeedConfigRead}},
	{OperationID: "listFeedConfigShedFactors", Method: "GET", Pattern: "/feed-config/shed-factors", Permissions: []string{FeedConfigRead}},
	// The experiment enroller's candidate source: the park's operational locations (each shed, and
	// each pen of a subdivided shed). A READ of authored location config, so FeedConfigRead -- the
	// same permission the screen's other reads carry.
	{OperationID: "listFeedConfigPens", Method: "GET", Pattern: "/feed-config/pens", Permissions: []string{FeedConfigRead}},
	{OperationID: "upsertFeedConfigRationRate", Method: "POST", Pattern: "/feed-config/ration-rates", Permissions: []string{FeedConfigWrite}},
	{OperationID: "createFeedConfigFeedItem", Method: "POST", Pattern: "/feed-config/feed-items", Permissions: []string{FeedConfigWrite}},
	// Retiring a feed item removes it from every future feed sheet, so it carries the same write
	// permission as authoring a rate -- it changes what animals are fed, not merely what a screen shows.
	{OperationID: "setFeedConfigFeedItemStatus", Method: "POST", Pattern: "/feed-config/feed-items/status", Permissions: []string{FeedConfigWrite}},
	{OperationID: "upsertFeedConfigShedFactor", Method: "POST", Pattern: "/feed-config/shed-factors", Permissions: []string{FeedConfigWrite}},
	// Atomic multi-item enrolment of ONE pen. Same permission as the single-cell write -- it is the
	// same authored surface -- but its own route because it carries an all-or-nothing guarantee.
	{OperationID: "upsertFeedConfigExperimentBatch", Method: "POST", Pattern: "/feed-config/experiment/batch", Permissions: []string{FeedConfigWrite}},
	{OperationID: "upsertFeedConfigSchedule", Method: "POST", Pattern: "/feed-config/schedule", Permissions: []string{FeedConfigWrite}},

	// The EXPERIMENT half of the same authored surface, on the same two permissions.
	//
	// It reuses FeedConfigRead/FeedConfigWrite rather than earning its own pair because it is the same
	// authority: whoever may rewrite the ration grid the whole farm is fed from may also decide which
	// sheds are fed absolute hand-entered kg instead. Splitting them would let a principal change what
	// a shed eats by moving it between the two workflows while nominally lacking grid-write authority
	// -- the same outcome through a different door.
	//
	// experiment/shed-status is a distinct write route, not a field on the cell write, so that the
	// workflow switch is an explicit act in both the API surface and the audit trail.
	{OperationID: "listFeedConfigExperiment", Method: "GET", Pattern: "/feed-config/experiment", Permissions: []string{FeedConfigRead}},
	{OperationID: "upsertFeedConfigExperiment", Method: "POST", Pattern: "/feed-config/experiment", Permissions: []string{FeedConfigWrite}},
	{OperationID: "setFeedConfigExperimentShedStatus", Method: "POST", Pattern: "/feed-config/experiment/shed-status", Permissions: []string{FeedConfigWrite}},

	{OperationID: "listCalendarVaccinationEvents", Method: "GET", Pattern: "/calendar/vaccination/events", Permissions: []string{CalendarRead}},
	{OperationID: "getCalendarVaccinationEvent", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}", Permissions: []string{CalendarRead}},
	{OperationID: "listCalendarVaccinationDriveTargets", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}/targets", Permissions: []string{CalendarRead}},
	{OperationID: "getCalendarVaccinationEventHistory", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}/history", Permissions: []string{CalendarRead}},
	{OperationID: "sendCalendarVaccinationEventNudge", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/nudge", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "snoozeCalendarVaccinationEvent", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/snooze", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "acknowledgeCalendarVaccinationEscalation", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/escalation/acknowledge", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "resolveCalendarVaccinationEscalation", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/escalation/resolve", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "vaccinationVerificationQueue", Method: "GET", Pattern: "/vaccination/verification-queue", Permissions: []string{VaccinationRead}},
	{OperationID: "getGoatVaccinationPassport", Method: "GET", Pattern: "/goats/{goat_id}/passport", Permissions: []string{GoatRead}},

	// Generic Verification vertical (context/architecture/verification-module-design.md): the
	// standalone Verifier queue read + approve/reject verdict write, split across THREE authorities
	// so no one holds two of them (separation of duty from capture via task.execute):
	//   verification.review  — SEE the evidence (Verifier + CEO/CxO leadership visibility)
	//   verification.verdict — DECIDE approve/reject (Verifier ONLY, maintainer decision 2026-08-03)
	//   verification.act     — CLOSE the work / act on the source task (leadership, not the Verifier)
	{OperationID: "listVerificationQueue", Method: "GET", Pattern: "/verification/queue", Permissions: []string{VerificationReview}},
	{OperationID: "listVerificationActionQueue", Method: "GET", Pattern: "/verification/action-queue", Permissions: []string{VerificationAct}},
	{OperationID: "listVerificationAlerts", Method: "GET", Pattern: "/verify/alerts", Permissions: []string{VerificationReview}},
	// Approve/reject belongs to the Verifier role ALONE (maintainer decision 2026-08-03).
	// Leadership keeps verification.review (see the queue, media and recorded verdicts) and
	// verification.act, but may not sign the second check itself.
	{OperationID: "recordVerificationVerdict", Method: "POST", Pattern: "/verification/items/{item_id}/verdict", Permissions: []string{VerificationVerdict}},
	{OperationID: "closeVerificationItem", Method: "POST", Pattern: "/verification/items/{item_id}/close", Permissions: []string{VerificationAct}},
	{OperationID: "closeVerificationSubmission", Method: "POST", Pattern: "/verification/submissions/{submission_id}/close", Permissions: []string{VerificationAct}},
	{OperationID: "closeVaccinationBatch", Method: "POST", Pattern: "/verification/vaccination-batches/{batch_id}/close", Permissions: []string{VerificationAct}},
	// Verifier video-review analytics (CEO integrity signal: did the verifier actually WATCH the
	// proof before deciding). Gated on verification.review -- the same permission that already
	// gates seeing the queue/evidence at all; this is telemetry ABOUT that same review activity,
	// never a separate write authority.
	// Telemetry INGEST is gated on VerificationVerdict, not VerificationReview. This stream measures
	// whether the person who signs the second check actually watched the evidence, and
	// VerificationReview is deliberately held by CEO/CxO for visibility -- so gating ingest on it let a
	// read-only leadership principal post queue/video/verdict telemetry under their own actor id,
	// polluting the integrity stream with rows for a principal who cannot record a verdict at all.
	// Reading the derived facts stays on VerificationReview: leadership must be able to SEE the signal
	// they cannot write. See context/architecture/verifier-app-and-flow.md -> Roles.
	{OperationID: "recordVerificationReviewEvents", Method: "POST", Pattern: "/verification/review-events", Permissions: []string{VerificationVerdict}},
	{OperationID: "getVerificationItemReviewFacts", Method: "GET", Pattern: "/verification/items/{item_id}/review-facts", Permissions: []string{VerificationReview}},
	// CEO/PC-Director oversight analytics (KPI strip, pending backlog by module, per-verifier
	// activity + watch integrity). Gated on verification.oversee -- the SAME capability as the
	// /verify page contract's oversight_analytics/oversight_filters controls, never role strings.
	// A verifier holds verification.review/verdict but NOT verification.oversee, so this route
	// 403s for her even though she can read the plain queue. See permissions.VerificationOversee.
	{OperationID: "getVerificationOversightAnalytics", Method: "GET", Pattern: "/verification/oversight-analytics", Permissions: []string{VerificationOversee}},

	// HR roster: staff positions (concept #2), leave/absence (#3), temporary
	// task coverage (#4), and the vaccination-ownership resolution read.
	{OperationID: "listStaffPositions", Method: "GET", Pattern: "/admin/roster/positions", Permissions: []string{RosterRead}},
	{OperationID: "createStaffPosition", Method: "POST", Pattern: "/admin/roster/positions", Permissions: []string{RosterManage}},
	{OperationID: "importStaffPositions", Method: "POST", Pattern: "/admin/roster/positions/import", Permissions: []string{RosterManage}},
	{OperationID: "getStaffPositionProfile", Method: "GET", Pattern: "/admin/roster/positions/{position_id}", Permissions: []string{RosterRead}},
	{OperationID: "updateStaffPosition", Method: "PATCH", Pattern: "/admin/roster/positions/{position_id}", Permissions: []string{RosterManage}},
	{OperationID: "upsertBackupConfig", Method: "POST", Pattern: "/admin/roster/backup-config", Permissions: []string{RosterManage}},
	{OperationID: "applyStaffLeave", Method: "POST", Pattern: "/admin/roster/leave", Permissions: []string{RosterManage}},
	{OperationID: "approveStaffLeave", Method: "POST", Pattern: "/admin/roster/leave/{absence_id}/approve", Permissions: []string{RosterManage}},
	{OperationID: "resolveStaffLeaveCoverage", Method: "POST", Pattern: "/admin/roster/leave/{absence_id}/resolve-coverage", Permissions: []string{RosterManage}},
	{OperationID: "getStaffLeave", Method: "GET", Pattern: "/admin/roster/leave/{absence_id}", Permissions: []string{RosterRead}},
	{OperationID: "listStaffLeave", Method: "GET", Pattern: "/admin/roster/leave", Permissions: []string{RosterRead}},
	{OperationID: "resolveVaccinationOwner", Method: "GET", Pattern: "/admin/roster/vaccination-owner", Permissions: []string{RosterRead}},
	{OperationID: "listBackupConfig", Method: "GET", Pattern: "/admin/roster/backup-config", Permissions: []string{RosterRead}},
	{OperationID: "listCoverage", Method: "GET", Pattern: "/admin/roster/coverage", Permissions: []string{RosterRead}},
	// App-tier operator-facing roster reads: gated on AppBootstrap = any authenticated
	// app user (operators + leadership all hold it), NOT the admin-tier RosterRead.
	// RolesAuthorize DENIES an empty required set, so "all authenticated" must name a
	// permission every app principal has — AppBootstrap is exactly that.
	{OperationID: "getOperatorTimetable", Method: "GET", Pattern: "/app/roster/timetable", Permissions: []string{AppBootstrap}},
	{OperationID: "getMyCoverage", Method: "GET", Pattern: "/app/roster/my-coverage", Permissions: []string{AppBootstrap}},

	// App-tier Counts write surface: the three count-moving events a field operator records from
	// the phone. Gated on the dedicated CountsWrite permission (see permissions.go) rather than on
	// the admin-tier goat.write_identity/goat.write_health, which RoleOperator deliberately lacks —
	// operators must be able to record births and deaths without also gaining every /admin/goats/*
	// route. Birth and death delegate to the identity module's existing CreateAdminGoat /
	// CriticalDeathExit services, so the guardrailed death semantics are unchanged.
	// The destination catalog is a READ that belongs to the write surface. It is gated on
	// CountsWrite, NOT on the admin-tier LocationsRead: the operator who must choose a destination
	// shed is exactly the operator who may record the movement, and RolesAuthorize ANDs a route's
	// permissions, so naming LocationsRead here would deny every operator who holds CountsWrite
	// alone -- leaving them able to submit a movement but unable to see where they may move it to.
	{OperationID: "listAppCountsShiftingDestinations", Method: "GET", Pattern: "/app/counts/shifting/destinations", Permissions: []string{CountsWrite}},
	// The birth form's breed picker is a READ on the write surface, gated on CountsWrite for the same
	// reason as the destinations catalog above: the operator who records a birth is exactly the
	// operator who picks the newborn's breed, and the read-only Counts Breakdown that also exposes
	// breeds is CountsRead (leadership-only). Naming CountsRead here would 403 every field operator.
	{OperationID: "listAppCountsBreeds", Method: "GET", Pattern: "/app/counts/breeds", Permissions: []string{CountsWrite}},
	{OperationID: "recordAppCountsShiftingEvent", Method: "POST", Pattern: "/app/counts/shifting-events", Permissions: []string{CountsWrite}},
	{OperationID: "recordAppCountsBirthEvent", Method: "POST", Pattern: "/app/counts/birth-events", Permissions: []string{CountsWrite}},
	{OperationID: "recordAppCountsDeathEvent", Method: "POST", Pattern: "/app/counts/death-events", Permissions: []string{CountsWrite}},
	{OperationID: "submitAppCountsMilkPreparation", Method: "POST", Pattern: "/app/counts/milk-preparation/submit", Permissions: []string{CountsWrite}},
	{OperationID: "listAppCountsMilkFeedingTasks", Method: "GET", Pattern: "/app/counts/milk-feeding/tasks", Permissions: []string{CountsWrite}},
	{OperationID: "submitAppCountsMilkFeedingTask", Method: "POST", Pattern: "/app/counts/milk-feeding/tasks/{task_id}/submit", Permissions: []string{CountsWrite}},
	{OperationID: "promoteAppCountsIdentifier", Method: "POST", Pattern: "/app/counts/goats/{goat_id}/promote-identifier", Permissions: []string{CountsWrite}},
	// The "Awaiting RFID" list is a READ on the write surface, gated on CountsWrite for the same
	// reason as the destinations catalog above: the operator who may promote is the operator who must
	// see the list, and CountsRead is leadership-only.
	{OperationID: "listAppCountsTemporaryTaggedGoats", Method: "GET", Pattern: "/app/counts/goats/temporary-tagged", Permissions: []string{CountsWrite}},

	// Counts lifecycle approval surface. The three routes above now RECORD a pending request; these
	// decide it.
	//
	// The route gate is the coarse CountsApproveAccess (see permissions.go): a decision addresses a
	// request by ID, so the middleware cannot know whether that ID is a birth or a shifting, and
	// Route.Permissions is ANDed, so naming both fine-grained permissions here would deny a
	// park_head who holds exactly one. The binding check is therefore made in the handler against
	// the request's STORED TYPE via DecidableApprovalRequestTypes -- a park_head reaching a birth's
	// id gets 403 there, and the pending list returns only the types the caller may decide.
	// Shifting EXECUTION surface (maintainer decision, 2026-07-19). Approving a shifting now
	// AUTHORIZES it and moves nothing; these three routes are the operator's half.
	//
	// Gated on CountsWrite, deliberately NOT on the approval permissions. Executing a movement is
	// ground work: the operator who walks the animals is the operator who records births, deaths,
	// and shiftings from the same phone, and is usually NOT the approver who authorized it. Any
	// operator holding CountsWrite may complete or cancel any authorized movement in their tenant
	// -- there is no "only the raiser" restriction, because the person who witnesses the animals
	// move is not reliably the person who typed the request.
	// Birth/death follow-up workflow surface (docs/decisions/birth-death-workflows.md). The per-goat
	// SOP work opened by an APPROVED birth/death is operator ground work from the same phone as the
	// Counts writes, so all four routes are gated on CountsWrite: the operator who records the birth
	// is the operator who runs the kid's follow-up checklist and shoots the death evidence videos.
	{OperationID: "listAppWorkflows", Method: "GET", Pattern: "/app/workflows", Permissions: []string{CountsWrite}},
	{OperationID: "getAppWorkflow", Method: "GET", Pattern: "/app/workflows/{workflow_id}", Permissions: []string{CountsWrite}},
	{OperationID: "answerAppWorkflowAction", Method: "POST", Pattern: "/app/workflows/{workflow_id}/actions/{action_id}/answer", Permissions: []string{CountsWrite}},
	{OperationID: "completeAppWorkflowAction", Method: "POST", Pattern: "/app/workflows/{workflow_id}/actions/{action_id}/complete", Permissions: []string{CountsWrite}},

	{OperationID: "listAppCountsShiftingPendingExecution", Method: "GET", Pattern: "/app/counts/shifting-events/pending-execution", Permissions: []string{CountsWrite}},
	{OperationID: "completeAppCountsShiftingEvent", Method: "POST", Pattern: "/app/counts/shifting-events/{shifting_event_id}/complete", Permissions: []string{CountsWrite}},
	{OperationID: "cancelAppCountsShiftingEvent", Method: "POST", Pattern: "/app/counts/shifting-events/{shifting_event_id}/cancel", Permissions: []string{CountsWrite}},

	{OperationID: "listAppCountsApprovals", Method: "GET", Pattern: "/app/counts/approvals", Permissions: []string{CountsApproveAccess}},
	{OperationID: "approveAppCountsApproval", Method: "POST", Pattern: "/app/counts/approvals/{request_id}/approve", Permissions: []string{CountsApproveAccess}},
	{OperationID: "rejectAppCountsApproval", Method: "POST", Pattern: "/app/counts/approvals/{request_id}/reject", Permissions: []string{CountsApproveAccess}},

	// admin-web Approvals page (maintainer decision 2026-07-21): the same decision surface served to
	// the four org tiers + admin + ceo_internal, plus anyone holding the per-person counts_approver
	// authority (maintainer decision 2026-08-05). Same coarse CountsApproveAccess route
	// gate; the per-type binding check stays in the handler via DecidableApprovalRequestTypes.
	{OperationID: "listAdminWebCountsApprovals", Method: "GET", Pattern: "/admin-web/counts/approvals", Permissions: []string{CountsApproveAccess}},
	{OperationID: "approveAdminWebCountsApproval", Method: "POST", Pattern: "/admin-web/counts/approvals/{request_id}/approve", Permissions: []string{CountsApproveAccess}},
	{OperationID: "rejectAdminWebCountsApproval", Method: "POST", Pattern: "/admin-web/counts/approvals/{request_id}/reject", Permissions: []string{CountsApproveAccess}},
}

// AuthorizeRoute is THE route-level authorization predicate. The HTTP auth
// middleware (internal/platform/httpmiddleware.AuthMiddleware.Wrap) calls this
// and nothing else, so a test that calls AuthorizeRoute exercises the exact
// production gate rather than a re-spelled copy of it.
//
// Permissions is ANDed (the caller must hold every listed permission).
// AnyPermissions is ORed (the caller must hold at least one) and is the form for
// an either/or surface such as the weighing planner reads, which the service
// layer already treats as "plan OR monitor". A route may set either list; when
// both are set both conditions must hold.
func AuthorizeRoute(route Route, roles []string) bool {
	if len(route.Permissions) == 0 && len(route.AnyPermissions) == 0 {
		return false
	}
	if route.AdminOnly {
		// Unchanged legacy semantics: AdminOnly short-circuits to the product-admin
		// role check and ignores the permission lists entirely.
		return RolesAuthorize(roles, route.Permissions, true)
	}
	if len(route.Permissions) > 0 && !RolesAuthorize(roles, route.Permissions, false) {
		return false
	}
	if len(route.AnyPermissions) > 0 && !RolesAuthorizeAny(roles, route.AnyPermissions) {
		return false
	}
	return true
}

func ProtectedRoutes() []Route {
	out := make([]Route, len(protectedRoutes))
	copy(out, protectedRoutes)
	return out
}

func Match(method, path string) (Route, bool) {
	for _, route := range protectedRoutes {
		if route.Method == method && pathMatches(route.Pattern, path) {
			return route, true
		}
	}
	return Route{}, false
}

func pathMatches(pattern, path string) bool {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		p := patternParts[i]
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if p != pathParts[i] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
