package permissions

import "strings"

type Route struct {
	OperationID string
	Method      string
	Pattern     string
	Permissions []string
	AdminOnly   bool
}

var protectedRoutes = []Route{
	{OperationID: "searchGoats", Method: "GET", Pattern: "/goats/search", Permissions: []string{GoatRead}},
	{OperationID: "getGoatPassport", Method: "GET", Pattern: "/goats/{goat_id}", Permissions: []string{GoatRead}},
	{OperationID: "getGoatTimeline", Method: "GET", Pattern: "/goats/{goat_id}/timeline", Permissions: []string{GoatRead}},
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
	{OperationID: "healthGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/health", Permissions: []string{GoatWriteHealth}},
	{OperationID: "reproductiveGoat", Method: "POST", Pattern: "/admin/goats/{goat_id}/reproductive", Permissions: []string{GoatWriteHealth}},
	// Bulk status-update kernel spans the reproductive/health/exit axes, so it
	// requires both the identity and health write grants (superuser data op).
	{OperationID: "previewBulkStatusUpdate", Method: "POST", Pattern: "/admin/goats/bulk-status/preview", Permissions: []string{GoatWriteIdentity, GoatWriteHealth}},
	{OperationID: "commitBulkStatusUpdate", Method: "POST", Pattern: "/admin/goats/bulk-status/commit", Permissions: []string{GoatWriteIdentity, GoatWriteHealth}},
	{OperationID: "listOperationsAudit", Method: "GET", Pattern: "/operations/audit", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "getOperationsAuditSummary", Method: "GET", Pattern: "/operations/audit/summary", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "getOperationsKernelHealth", Method: "GET", Pattern: "/operations/kernel-health", Permissions: []string{OperatorsViewAudit}},
	{OperationID: "listOutboxDLQ", Method: "GET", Pattern: "/operations/dlq", Permissions: []string{OperatorsViewAudit}},
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
	// Mobile live remote-config poll (docs/mobile/backend-driven-config.md): ETag/revision +
	// cache_policy, presentation feature flags/owned-module registry, and bounded client runtime
	// knobs. Same AppBootstrap "any authenticated app principal" gate as /app/bootstrap.
	{OperationID: "appConfig", Method: "GET", Pattern: "/app/config", Permissions: []string{AppBootstrap}},

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
	{OperationID: "createProofUpload", Method: "POST", Pattern: "/app/proofs/uploads", Permissions: []string{TaskExecute}},
	{OperationID: "uploadProofLocal", Method: "PUT", Pattern: "/app/proofs/{proof_id}/upload", Permissions: []string{TaskExecute}},
	{OperationID: "completeProofUpload", Method: "POST", Pattern: "/app/proofs/{proof_id}/complete", Permissions: []string{TaskExecute}},
	{OperationID: "downloadProof", Method: "GET", Pattern: "/app/proofs/{proof_id}/download", Permissions: []string{TaskRead}},
	{OperationID: "submitAppTask", Method: "POST", Pattern: "/app/tasks/{task_id}/submissions", Permissions: []string{TaskExecute}},

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
	{OperationID: "getVaccinationProtocolAdherence", Method: "GET", Pattern: "/vaccination/adherence", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationControlTower", Method: "GET", Pattern: "/control-tower/vaccination", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getWorkflowDrilldown", Method: "GET", Pattern: "/workflows/{row_id}", Permissions: []string{ObligationRead}},
	{OperationID: "getVaccinationWorkflowDrilldown", Method: "GET", Pattern: "/vaccination/workflows/{row_id}", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationOperations", Method: "GET", Pattern: "/vaccination/operations", Permissions: []string{ObligationRead, VaccinationRead}},
	{OperationID: "listVaccinationExecution", Method: "GET", Pattern: "/vaccination/execution", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationExecutionShedDrilldown", Method: "GET", Pattern: "/vaccination/execution/sheds/{shed_id}", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "listVaccinationShedSummary", Method: "GET", Pattern: "/vaccination/sheds", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationShedDetail", Method: "GET", Pattern: "/vaccination/sheds/{shed_id}", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationShedAnimals", Method: "GET", Pattern: "/vaccination/sheds/{shed_id}/animals", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{OperationID: "getVaccinationCapacityConfig", Method: "GET", Pattern: "/vaccination/capacity-config", Permissions: []string{ProtocolRead}},
	{OperationID: "updateVaccinationCapacityConfig", Method: "PUT", Pattern: "/vaccination/capacity-config", Permissions: []string{ProtocolWrite}},
	// App-tier vaccination execution: gated on AppBootstrap = any authenticated
	// app user (operators + leadership all hold it), NOT the admin-tier
	// LocationsRead/ObligationRead/VaccinationRead/CalendarAction combo RoleOperator
	// lacks. Mirrors the /app/roster/* precedent (commit 6c8962be) -- these are the
	// core operator app actions (scan the shed roster, reschedule an obligation),
	// so gating them on admin-tier perms 403s every field operator. The admin
	// /vaccination/* routes above stay on their existing admin-tier perms.
	{OperationID: "appScanRoster", Method: "GET", Pattern: "/app/vaccination/execution/sheds/{shed_id}/roster", Permissions: []string{AppBootstrap}},
	{OperationID: "appRescheduleObligation", Method: "POST", Pattern: "/app/vaccination/obligations/{obligation_id}/reschedule", Permissions: []string{AppBootstrap}},
	// App-tier data-gaps + coverage-rollup overlays: same AppBootstrap "any authenticated app
	// principal" gate as appScanRoster/appRescheduleObligation above (mobile Data gaps + Doses
	// given overlays, Overlays.kt DataGapsSheet/DosesGivenSheet).
	{OperationID: "appVaccinationGaps", Method: "GET", Pattern: "/app/vaccination/gaps", Permissions: []string{AppBootstrap}},
	{OperationID: "appVaccinationCoverage", Method: "GET", Pattern: "/app/vaccination/coverage", Permissions: []string{AppBootstrap}},
	{OperationID: "getFeedDirectionReadiness", Method: "GET", Pattern: "/feed-direction/readiness", Permissions: []string{ProtocolRead}},
	{OperationID: "getFeedDirectionGenerationPreview", Method: "GET", Pattern: "/feed-direction/generation-preview", Permissions: []string{ProtocolRead}},
	{OperationID: "listFeedDirectionCountsProjectionExceptions", Method: "GET", Pattern: "/feed-direction/counts-projection/exceptions", Permissions: []string{ProtocolRead}},
	{OperationID: "resolveFeedDirectionCountsProjectionException", Method: "POST", Pattern: "/feed-direction/counts-projection/exceptions/{exception_id}/resolve", Permissions: []string{ProtocolWrite}},
	{OperationID: "dismissFeedDirectionCountsProjectionException", Method: "POST", Pattern: "/feed-direction/counts-projection/exceptions/{exception_id}/dismiss", Permissions: []string{ProtocolWrite}},
	{OperationID: "listCalendarVaccinationEvents", Method: "GET", Pattern: "/calendar/vaccination/events", Permissions: []string{CalendarRead, VaccinationRead, ObligationRead}},
	{OperationID: "getCalendarVaccinationEvent", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}", Permissions: []string{CalendarRead, VaccinationRead, ObligationRead}},
	{OperationID: "listCalendarVaccinationDriveTargets", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}/targets", Permissions: []string{CalendarRead, VaccinationRead, ObligationRead}},
	{OperationID: "getCalendarVaccinationEventHistory", Method: "GET", Pattern: "/calendar/vaccination/events/{event_id}/history", Permissions: []string{CalendarRead, VaccinationRead, ObligationRead}},
	{OperationID: "sendCalendarVaccinationEventNudge", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/nudge", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "snoozeCalendarVaccinationEvent", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/snooze", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "acknowledgeCalendarVaccinationEscalation", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/escalation/acknowledge", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "resolveCalendarVaccinationEscalation", Method: "POST", Pattern: "/calendar/vaccination/events/{event_id}/escalation/resolve", Permissions: []string{CalendarAction, VaccinationRead, ObligationRead}},
	{OperationID: "vaccinationVerificationQueue", Method: "GET", Pattern: "/vaccination/verification-queue", Permissions: []string{VaccinationRead}},
	{OperationID: "getGoatVaccinationPassport", Method: "GET", Pattern: "/goats/{goat_id}/passport", Permissions: []string{GoatRead}},

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
