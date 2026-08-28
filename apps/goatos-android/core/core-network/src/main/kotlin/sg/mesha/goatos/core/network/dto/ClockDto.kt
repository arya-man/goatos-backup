package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Clock In / Clock Out module (docs/features/clock-in-out/plan.md §4). Every person starts the
 * working day by clocking in and ends it by clocking out; the backend owns one number from the
 * pair — hours worked that IST business day. ALL screen copy (state lines, button labels, section
 * headers, empty states, the shell reminder banner) is BACKEND-OWNED via the `copy` map on the
 * status/presence responses and rendered verbatim.
 *
 * Wire contract of record: contracts/openapi/app-api.yaml, tag "Clock".
 */
@Serializable
data class ClockLocationDto(
    /** `captured` | `permission_missing` | `unavailable`. */
    @SerialName("status") val status: String,
    @SerialName("latitude") val latitude: Double? = null,
    @SerialName("longitude") val longitude: Double? = null,
    @SerialName("gps_accuracy_m") val gpsAccuracyM: Double? = null,
    @SerialName("address") val address: String? = null,
)

@Serializable
data class ClockIntegrityDto(
    /** True when the captured fix itself is mock-provided. The server refuses such a punch. */
    @SerialName("mock_location") val mockLocation: Boolean,
    /** Installed fake-GPS apps found by the punch-time package scan. */
    @SerialName("mock_provider_packages") val mockProviderPackages: List<String> = emptyList(),
    /** Recorded, never blocking: blocking on developer options would lock out dev/test phones. */
    @SerialName("developer_options_enabled") val developerOptionsEnabled: Boolean? = null,
)

@Serializable
data class ClockPunchRequestDto(
    @SerialName("idempotency_key") val idempotencyKey: String,
    /** Device clock at tap time (RFC3339). The server stamps its own arrival + skew. */
    @SerialName("captured_at") val capturedAt: String,
    /** True when captured without network and drained later by the outbox. */
    @SerialName("offline") val offline: Boolean,
    @SerialName("location") val location: ClockLocationDto,
    @SerialName("integrity") val integrity: ClockIntegrityDto,
    @SerialName("battery_pct") val batteryPct: Int? = null,
    /** `wifi` | `cellular` when online; informational. */
    @SerialName("network_kind") val networkKind: String? = null,
)

/** Backend-composed flag chip ("Offline punch", "No location", "Not clocked out"), VERBATIM. */
@Serializable
data class ClockFlagDto(
    @SerialName("key") val key: String,
    @SerialName("label") val label: String,
)

/** One person's day entry — the paired in/out read model row. Labels are backend-composed IST. */
@Serializable
data class ClockEntryDto(
    @SerialName("clock_entry_id") val clockEntryId: String,
    @SerialName("workforce_member_id") val workforceMemberId: String,
    @SerialName("person_name") val personName: String,
    @SerialName("role_hint") val roleHint: String = "",
    @SerialName("designation") val designation: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("department_label") val departmentLabel: String = "",
    @SerialName("business_date") val businessDate: String,
    /** `open` | `closed` | `auto_closed`; empty for a not-clocked-in row. */
    @SerialName("status") val status: String,
    @SerialName("clock_in_at") val clockInAt: String,
    /** Backend-composed IST time label ("08:12"), rendered verbatim. */
    @SerialName("clock_in_label") val clockInLabel: String,
    @SerialName("clock_out_at") val clockOutAt: String? = null,
    @SerialName("clock_out_label") val clockOutLabel: String? = null,
    /** Backend-owned hours truth; absent while open and forever on auto_closed. */
    @SerialName("worked_minutes") val workedMinutes: Int? = null,
    @SerialName("hours_label") val hoursLabel: String? = null,
    /** The clock-in punch's reverse-geocoded address, backend-selected. */
    @SerialName("location_label") val locationLabel: String = "",
    /** Backend-composed "device model · app version" at clock-in. */
    @SerialName("device_label") val deviceLabel: String = "",
    @SerialName("flags") val flags: List<ClockFlagDto> = emptyList(),
)

@Serializable
data class ClockPunchResponseDto(
    @SerialName("entry") val entry: ClockEntryDto,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class ClockStatusResponseDto(
    @SerialName("business_date") val businessDate: String,
    /** `not_clocked_in` | `clocked_in` | `clocked_out`. */
    @SerialName("state") val state: String,
    @SerialName("entry") val entry: ClockEntryDto? = null,
    /** Backend-owned shell reminder; EMPTY means no banner. Rendered verbatim, never composed. */
    @SerialName("banner_text") val bannerText: String = "",
    /** Farm-worded refusal template; %s is the offending app label list. */
    @SerialName("punch_refused_copy") val punchRefusedCopy: String = "",
    /** The module's whole label set — every visible word on the clock screens comes from here. */
    @SerialName("copy") val copy: Map<String, String> = emptyMap(),
    @SerialName("recent_entries") val recentEntries: List<ClockEntryDto> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
)

/** Whole-filter summary tiles; `flagged` counts flagged ENTRIES and overlaps the other buckets. */
@Serializable
data class ClockPresenceSummaryDto(
    @SerialName("working") val working: Int = 0,
    @SerialName("clocked_out") val clockedOut: Int = 0,
    @SerialName("not_clocked_in") val notClockedIn: Int = 0,
    @SerialName("flagged") val flagged: Int = 0,
)

@Serializable
data class ClockPresenceRowDto(
    @SerialName("clock_entry_id") val clockEntryId: String = "",
    @SerialName("workforce_member_id") val workforceMemberId: String,
    @SerialName("person_name") val personName: String,
    @SerialName("designation") val designation: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    /** `working` | `clocked_out` | `not_clocked_in`. */
    @SerialName("bucket") val bucket: String,
    /** Backend-composed row line ("In 08:12", "08:02 – 17:31 · 9h 29m"), rendered VERBATIM. */
    @SerialName("time_label") val timeLabel: String = "",
    @SerialName("clock_in_at") val clockInAt: String = "",
    @SerialName("clock_out_at") val clockOutAt: String = "",
    @SerialName("worked_minutes") val workedMinutes: Int? = null,
    @SerialName("flags") val flags: List<ClockFlagDto> = emptyList(),
)

@Serializable
data class ClockFilterOptionDto(
    @SerialName("id") val id: String,
    @SerialName("code") val code: String,
    @SerialName("label") val label: String,
)

@Serializable
data class ClockPresenceResponseDto(
    @SerialName("business_date") val businessDate: String,
    @SerialName("is_today") val isToday: Boolean = true,
    @SerialName("summary") val summary: ClockPresenceSummaryDto = ClockPresenceSummaryDto(),
    @SerialName("rows") val rows: List<ClockPresenceRowDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    @SerialName("parks") val parks: List<ClockFilterOptionDto> = emptyList(),
    @SerialName("designations") val designations: List<ClockFilterOptionDto> = emptyList(),
    @SerialName("copy") val copy: Map<String, String> = emptyMap(),
    @SerialName("trace_id") val traceId: String = "",
)

/** One punch event in full — everything the device honestly told us at tap time. */
@Serializable
data class ClockEventDetailDto(
    @SerialName("clock_event_id") val clockEventId: String,
    @SerialName("event_type") val eventType: String,
    @SerialName("business_date") val businessDate: String,
    @SerialName("captured_at") val capturedAt: String,
    @SerialName("recorded_at") val recordedAt: String,
    @SerialName("clock_skew_ms") val clockSkewMs: Long? = null,
    @SerialName("location_status") val locationStatus: String,
    @SerialName("latitude") val latitude: Double? = null,
    @SerialName("longitude") val longitude: Double? = null,
    @SerialName("gps_accuracy_m") val gpsAccuracyM: Double? = null,
    @SerialName("address") val address: String = "",
    @SerialName("mock_location") val mockLocation: Boolean = false,
    @SerialName("developer_options_enabled") val developerOptionsEnabled: Boolean? = null,
    @SerialName("device_model") val deviceModel: String = "",
    @SerialName("app_version") val appVersion: String = "",
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("network_type") val networkType: String = "",
    @SerialName("battery_pct") val batteryPct: Int? = null,
)

@Serializable
data class ClockPersonDayResponseDto(
    @SerialName("person_name") val personName: String,
    @SerialName("designation") val designation: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("business_date") val businessDate: String,
    @SerialName("entry") val entry: ClockEntryDto? = null,
    @SerialName("events") val events: List<ClockEventDetailDto> = emptyList(),
    @SerialName("recent_days") val recentDays: List<ClockEntryDto> = emptyList(),
    @SerialName("copy") val copy: Map<String, String> = emptyMap(),
    @SerialName("trace_id") val traceId: String = "",
)
