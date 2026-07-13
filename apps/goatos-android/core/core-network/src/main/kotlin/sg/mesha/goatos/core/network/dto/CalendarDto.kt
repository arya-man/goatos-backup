package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * GET /calendar/vaccination/events -> CalendarEventListResponse (presentation + items[]).
 * snake_case wire format.
 *
 * The full CalendarPresentation carries rhythm/week/month/new_event sub-objects that
 * the mobile list does not render; those are dropped by ignoreUnknownKeys. Only the
 * segments the mobile Calendar screen needs (title/subtitle, view/owner/workstream
 * tabs, empty state, event-type legend, active-owner header) are modeled.
 */
@Serializable
data class CalendarPresentationTabDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("active") val active: Boolean = false,
    @SerialName("enabled") val enabled: Boolean = true,
    @SerialName("disabled_reason") val disabledReason: String = "",
    // CalendarPresentationQuery = free-form string->string map of query params.
    @SerialName("query") val query: Map<String, String> = emptyMap(),
)

@Serializable
data class CalendarOwnerPresentationTabDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("scope_label") val scopeLabel: String = "",
    @SerialName("color") val color: String = "",
    @SerialName("active") val active: Boolean = false,
    @SerialName("enabled") val enabled: Boolean = true,
    @SerialName("disabled_reason") val disabledReason: String = "",
    @SerialName("query") val query: Map<String, String> = emptyMap(),
)

@Serializable
data class CalendarEmptyStatePresentationDto(
    @SerialName("ok_message") val okMessage: String = "",
    @SerialName("error_message") val errorMessage: String = "",
    @SerialName("primary_label") val primaryLabel: String = "",
    @SerialName("secondary_label") val secondaryLabel: String = "",
)

@Serializable
data class CalendarKeyLabelDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class CalendarDateMarkerDto(
    @SerialName("date") val date: String = "",
    @SerialName("event_count") val eventCount: Int = 0,
    @SerialName("completed_count") val completedCount: Int = 0,
    @SerialName("open_count") val openCount: Int = 0,
    @SerialName("drive_count") val driveCount: Int = 0,
)

/**
 * Park-level vaccination-drive progress (v4). A drive clubs multiple sheds and vaccines
 * under one park scope; this replaces the old per-item shed/vaccine/dose tile trio with a
 * single progress summary the Calendar card renders as-is (no client-side aggregation —
 * mobile-guard: this is a straight field pass-through, never a fetch/rollup of the
 * underlying target rows). Nullable/absent on [CalendarEventDto] until the backend ships
 * `drive_summary`; the screen falls back to the legacy tiles in that case.
 */
@Serializable
data class DriveSummaryDto(
    @SerialName("park_name") val parkName: String = "",
    @SerialName("due_date") val dueDate: String = "",
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("sheds_completed") val shedsCompleted: Int = 0,
    @SerialName("vaccine_labels") val vaccineLabels: List<String> = emptyList(),
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("completed_count") val completedCount: Int = 0,
    @SerialName("remaining_count") val remainingCount: Int = 0,
    @SerialName("due_count") val dueCount: Int = 0,
    @SerialName("overdue_count") val overdueCount: Int = 0,
    @SerialName("deferred_count") val deferredCount: Int = 0,
    @SerialName("blocked_count") val blockedCount: Int = 0,
    @SerialName("owner_label") val ownerLabel: String = "",
)

@Serializable
data class CalendarPresentationDto(
    @SerialName("page_title") val pageTitle: String = "",
    @SerialName("page_subtitle") val pageSubtitle: String = "",
    @SerialName("view_tabs") val viewTabs: List<CalendarPresentationTabDto> = emptyList(),
    @SerialName("owner_tabs") val ownerTabs: List<CalendarOwnerPresentationTabDto> = emptyList(),
    @SerialName("workstream_tabs") val workstreamTabs: List<CalendarPresentationTabDto> = emptyList(),
    @SerialName("empty_state") val emptyState: CalendarEmptyStatePresentationDto = CalendarEmptyStatePresentationDto(),
    @SerialName("event_types") val eventTypes: List<CalendarKeyLabelDto> = emptyList(),
    @SerialName("active_owner_key") val activeOwnerKey: String = "",
    @SerialName("active_owner_label") val activeOwnerLabel: String = "",
    @SerialName("active_owner_scope_label") val activeOwnerScopeLabel: String = "",
    @SerialName("active_owner_color") val activeOwnerColor: String = "",
    @SerialName("all_owners_selected_label") val allOwnersSelectedLabel: String = "",
)

/**
 * CalendarEvent. Enums (event_type, owner_key, status, severity) kept as String.
 * `links` is a free-form map whose values are string|boolean|null per the contract
 * (CalendarEventLinks); modeled as JsonElement values so any value type parses.
 * Common keys observed: "drive", "vaccination" (route hrefs for row navigation).
 */
@Serializable
data class CalendarEventDto(
    @SerialName("event_id") val eventId: String = "",
    @SerialName("event_type") val eventType: String = "",
    @SerialName("owner_key") val ownerKey: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("subtitle") val subtitle: String = "",
    @SerialName("aggregated") val aggregated: Boolean = false,
    @SerialName("all_day") val allDay: Boolean = false,
    @SerialName("summary_primary") val summaryPrimary: String = "",
    @SerialName("summary_secondary") val summarySecondary: String = "",
    @SerialName("summary_tertiary") val summaryTertiary: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("severity") val severity: String = "",
    @SerialName("due_at") val dueAt: String = "",
    @SerialName("window_start") val windowStart: String? = null,
    @SerialName("window_end") val windowEnd: String? = null,
    @SerialName("timezone") val timezone: String = "",
    @SerialName("timezone_source") val timezoneSource: String = "",
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_code") val parkCode: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_name") val shedName: String? = null,
    @SerialName("cohort_id") val cohortId: String? = null,
    @SerialName("cohort_name") val cohortName: String? = null,
    @SerialName("target_type") val targetType: String = "",
    @SerialName("target_count") val targetCount: Int = 0,
    @SerialName("shed_count") val shedCount: Int = 0,
    @SerialName("vaccine_count") val vaccineCount: Int = 0,
    @SerialName("drive_count") val driveCount: Int = 0,
    @SerialName("catch_up_count") val catchUpCount: Int = 0,
    @SerialName("scheduled_count") val scheduledCount: Int = 0,
    @SerialName("deferred_count") val deferredCount: Int = 0,
    @SerialName("review_count") val reviewCount: Int = 0,
    @SerialName("shed_labels") val shedLabels: List<String> = emptyList(),
    @SerialName("vaccine_labels") val vaccineLabels: List<String> = emptyList(),
    // Park-level drive progress (v4) — see [DriveSummaryDto]. Null until the backend ships it.
    @SerialName("drive_summary") val driveSummary: DriveSummaryDto? = null,
    @SerialName("protocol_id") val protocolId: String? = null,
    @SerialName("protocol_version_id") val protocolVersionId: String? = null,
    @SerialName("rule_id") val ruleId: String? = null,
    @SerialName("vaccine_name") val vaccineName: String? = null,
    @SerialName("dose_code") val doseCode: String? = null,
    @SerialName("source_backed") val sourceBacked: Boolean = false,
    @SerialName("source_label") val sourceLabel: String = "",
    @SerialName("assignee_label") val assigneeLabel: String? = null,
    @SerialName("executor_role") val executorRole: String? = null,
    @SerialName("verifier_label") val verifierLabel: String? = null,
    @SerialName("reminder_state") val reminderState: String = "",
    @SerialName("primary_notification_channel") val primaryNotificationChannel: String = "",
    @SerialName("escalation_state") val escalationState: String = "",
    @SerialName("system") val system: Boolean = false,
    @SerialName("cross_cutting") val crossCutting: Boolean = false,
    @SerialName("links") val links: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class CalendarEventListResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("presentation") val presentation: CalendarPresentationDto = CalendarPresentationDto(),
    @SerialName("items") val items: List<CalendarEventDto> = emptyList(),
    @SerialName("date_markers") val dateMarkers: List<CalendarDateMarkerDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)
