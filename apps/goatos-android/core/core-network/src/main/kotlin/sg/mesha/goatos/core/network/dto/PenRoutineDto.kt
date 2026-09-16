package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/**
 * Pen routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md): a routine is
 * a rule the CEO writes once per park ("every day, for every occupied pen, the park head answers
 * 'Was the pen cleaned?', takes one photo and checks in to the pen first"); the kernel raises ONE
 * task per pen per occurrence and the assignee works each card: check in -> answer -> capture ->
 * submit. The form the phone renders is the routine VERSION the task was raised under, so the
 * questions and capture rules ride on the task itself.
 *
 * ALL visible copy (`title`, `operational_location_display`, `reason_line`, `state_chip`,
 * `instruction`, `evidence_line`, `presence_line`, `done_line`, `rework_reason`, every question
 * title/hint/option label, every filter label and empty line) is BACKEND-OWNED and rendered
 * verbatim. The phone composes layout only.
 *
 * Wire contract of record: backend/internal/penroutines/domain.Step (the task) and
 * domain.Evidence / Question / Option / ProofRule (the form).
 */
@Serializable
data class PenRoutineOptionDto(
    @SerialName("value") val value: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class PenRoutineQuestionDto(
    @SerialName("id") val id: String = "",
    /** `yes_no` | `choice` | `multi_choice` | `number` | `text`. */
    @SerialName("kind") val kind: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("hint") val hint: String = "",
    @SerialName("required") val required: Boolean = false,
    @SerialName("options") val options: List<PenRoutineOptionDto> = emptyList(),
    @SerialName("min") val min: Double? = null,
    @SerialName("max") val max: Double? = null,
    @SerialName("unit") val unit: String = "",
)

@Serializable
data class PenRoutineProofRuleDto(
    @SerialName("min") val min: Int = 0,
    @SerialName("max") val max: Int = 0,
)

@Serializable
data class PenRoutineFormDto(
    @SerialName("questions") val questions: List<PenRoutineQuestionDto> = emptyList(),
    @SerialName("photo") val photo: PenRoutineProofRuleDto = PenRoutineProofRuleDto(),
    @SerialName("video") val video: PenRoutineProofRuleDto = PenRoutineProofRuleDto(),
    /** `required` | `off`. */
    @SerialName("presence") val presence: String = "off",
)

/** One stored answer as the backend renders it (question order, unanswered skipped). */
@Serializable
data class PenRoutineAnswerRowDto(
    @SerialName("question_id") val questionId: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("value") val value: String = "",
)

/** One capture on a task: a server proof id and its kind (`photo` | `video`). */
@Serializable
data class PenRoutineProofDto(
    @SerialName("ref") val ref: String = "",
    @SerialName("kind") val kind: String = "",
)

@Serializable
data class PenRoutineTaskDto(
    @SerialName("task_id") val taskId: String = "",
    @SerialName("routine_id") val routineId: String = "",
    @SerialName("routine_version") val routineVersion: Int = 0,
    @SerialName("routine_name") val routineName: String = "",
    /** Backend-composed ("Pen cleaning · Castro 2 · Coimbatore"), rendered VERBATIM. */
    @SerialName("title") val title: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_name") val parkName: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    /** The pen label as the backend composed it — rendered VERBATIM, never re-composed. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("trigger_kinds") val triggerKinds: List<String> = emptyList(),
    @SerialName("reason_line") val reasonLine: String = "",
    @SerialName("source_business_date") val sourceBusinessDate: String = "",
    @SerialName("planned_business_date") val plannedBusinessDate: String = "",
    @SerialName("due_business_date") val dueBusinessDate: String = "",
    /** `scheduled` | `delayed` | `completed` | `canceled` -- the kernel clock. */
    @SerialName("work_state") val workState: String = "",
    /** `open` | `pending_verification` | `completed` | `rework` -- the gate. */
    @SerialName("status") val status: String = "",
    @SerialName("state_chip") val stateChip: String = "",
    /** `info` | `review` | `danger` | `success` | `muted` — the chip's colour only. */
    @SerialName("state_tone") val stateTone: String = "",
    @SerialName("instruction") val instruction: String = "",
    @SerialName("evidence_line") val evidenceLine: String = "",
    /** `verifier` | `none`. */
    @SerialName("review_kind") val reviewKind: String = "",
    @SerialName("form") val form: PenRoutineFormDto = PenRoutineFormDto(),
    /** The stored answers keyed by question id; null when none. Kept opaque: the phone edits its
     *  own draft and renders [answerRows], which the backend composes. */
    @SerialName("answers") val answers: JsonObject? = null,
    @SerialName("answer_rows") val answerRows: List<PenRoutineAnswerRowDto> = emptyList(),
    @SerialName("proofs") val proofs: List<PenRoutineProofDto> = emptyList(),
    @SerialName("presence_required") val presenceRequired: Boolean = false,
    /** Backend-composed ("Check in to the pen before you start" / "In pen since 07:12"), VERBATIM. */
    @SerialName("presence_line") val presenceLine: String = "",
    @SerialName("in_pen") val inPen: Boolean = false,
    @SerialName("can_check_in") val canCheckIn: Boolean = false,
    @SerialName("entered_at") val enteredAt: String? = null,
    @SerialName("left_at") val leftAt: String? = null,
    /** The caller is an assignee and work is still owed — backend-resolved. */
    @SerialName("can_submit") val canSubmit: Boolean = false,
    @SerialName("verified") val verified: Boolean = false,
    @SerialName("done_line") val doneLine: String = "",
    /** The verifier's words when the task was sent back, VERBATIM; blank otherwise. */
    @SerialName("rework_reason") val reworkReason: String = "",
    @SerialName("submitted_at") val submittedAt: String? = null,
    @SerialName("verified_at") val verifiedAt: String? = null,
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class PenRoutineDetailDto(
    @SerialName("task") val task: PenRoutineTaskDto = PenRoutineTaskDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** One filter chip on the list. Labels, counts and the empty line are backend-composed. */
@Serializable
data class PenRoutineFilterDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("count") val count: Int = 0,
    @SerialName("selected") val selected: Boolean = false,
    @SerialName("empty_message") val emptyMessage: String = "",
)

@Serializable
data class PenRoutinePageDto(
    /** The backend's own page title ("Routines"), rendered VERBATIM. */
    @SerialName("title") val title: String = "",
    @SerialName("rows") val rows: List<PenRoutineTaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("filters") val filters: List<PenRoutineFilterDto> = emptyList(),
    /** Whole-list count of tasks still owed by the caller; never a page-local sum. */
    @SerialName("open_count") val openCount: Int = 0,
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * What the phone captured when the presence punch (or the submit) was made — the workforce
 * clock's honest-capture location block. Every field is optional and recorded as given: V1
 * refuses nothing by distance.
 */
@Serializable
data class PenRoutineLocationDto(
    @SerialName("latitude") val latitude: Double? = null,
    @SerialName("longitude") val longitude: Double? = null,
    @SerialName("accuracy_m") val accuracyM: Double? = null,
    /** `captured` | `permission_missing` | `unavailable`. */
    @SerialName("status") val status: String = "",
    @SerialName("address") val address: String? = null,
)

/** The honest-capture integrity block the workforce clock also records. */
@Serializable
data class PenRoutineIntegrityDto(
    @SerialName("mock_location") val mockLocation: Boolean? = null,
    @SerialName("device_id") val deviceId: String? = null,
    @SerialName("app_version") val appVersion: String? = null,
    @SerialName("device_model") val deviceModel: String? = null,
    @SerialName("offline") val offline: Boolean? = null,
)

/**
 * `POST /app/pen-routines/{task_id}/presence` body: the `enter` punch (the `leave` is stamped by
 * the submit itself). [rowVersion] is the optimistic-concurrency token the screen last rendered.
 */
@Serializable
data class PenRoutinePresenceRequestDto(
    /** `enter` | `leave`. */
    @SerialName("event_type") val eventType: String,
    /** Device clock at tap time (RFC3339). */
    @SerialName("captured_at") val capturedAt: String,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("location") val location: PenRoutineLocationDto,
    @SerialName("integrity") val integrity: PenRoutineIntegrityDto,
)

/**
 * `POST /app/pen-routines/{task_id}/submit` body. [answers] is keyed by question id — yes_no /
 * choice carry the option value as a string, multi_choice a string array, number a number, text a
 * string. [proofRefs] are SERVER proof ids of the uploaded in-app-camera captures.
 */
@Serializable
data class PenRoutineSubmitRequestDto(
    @SerialName("answers") val answers: JsonObject,
    @SerialName("proof_refs") val proofRefs: List<PenRoutineProofDto>,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("captured_at") val capturedAt: String? = null,
    @SerialName("location") val location: PenRoutineLocationDto? = null,
    @SerialName("integrity") val integrity: PenRoutineIntegrityDto? = null,
)
