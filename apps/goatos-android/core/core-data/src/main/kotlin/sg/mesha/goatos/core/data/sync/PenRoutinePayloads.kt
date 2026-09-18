package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import sg.mesha.goatos.core.network.dto.PenRoutineIntegrityDto
import sg.mesha.goatos.core.network.dto.PenRoutineLocationDto

/**
 * Outbox payloads and the ONE definition of every pen-routine outbox key (maintainer instruction
 * 2026-09-16, docs/decisions/pen-routines.md). Every idempotency key, group key and capture
 * field key a routine write uses is built HERE and nowhere else — a second call site rebuilding
 * a key is exactly how the feed "In review" badge broke three separate ways (see
 * SubmittedGrainKeys.kt's header).
 */

/** Group key for a task's presence punch, its PROOF_UPLOAD rows and its submit — one FIFO lane
 *  per task, so the check-in lands first, every upload drains BEFORE the submit that references
 *  it, and the submit goes last. The capture path must pass this SAME value as its
 *  `uploadGroupKey` when enqueueing a slot's photo or video. */
fun penRoutineTaskGroupKey(taskId: String): String = "pen-routine:task:$taskId"

/** STABLE per (task, event, row version): a retry replays for free, while a punch after the task
 *  moved (bumped row_version) is a genuinely new act under a new key. Never timestamp-suffixed. */
fun penRoutinePresenceIdempotencyKey(taskId: String, eventType: String, rowVersion: Int): String =
    "pen-routine:presence:$taskId:$eventType:$rowVersion"

/** STABLE per (task, row version) — same rule as the presence key. */
fun penRoutineSubmitIdempotencyKey(taskId: String, rowVersion: Int): String =
    "pen-routine:submit:$taskId:$rowVersion"

/** Wire proof kinds (backend/internal/penroutines/domain: ProofKindPhoto / ProofKindVideo). */
const val PEN_ROUTINE_PROOF_KIND_PHOTO = "photo"
const val PEN_ROUTINE_PROOF_KIND_VIDEO = "video"

/** The capture identity's field key for photo slot [index] (1-based): ONE capture per slot,
 *  replaced on a re-take. */
fun penRoutinePhotoFieldKey(index: Int): String = "routine-photo-$index"

/** The capture identity's field key for video slot [index] (1-based). */
fun penRoutineVideoFieldKey(index: Int): String = "routine-video-$index"

/**
 * The field key of a capture that answers ONE QUESTION (maintainer instruction 2026-09-18):
 * `routine-q-<question id>-<photo|video>-<index>`. Question ids are letters, digits and
 * underscores on the server, so the key parses back unambiguously.
 */
fun penRoutineQuestionProofFieldKey(questionId: String, kind: String, index: Int): String =
    "routine-q-$questionId-$kind-$index"

/** The parsed parts of a question-proof field key, or null for any other key. */
data class PenRoutineQuestionProofKey(val questionId: String, val kind: String, val index: Int)

private val questionProofKeyPattern = Regex("^routine-q-([A-Za-z0-9_]+)-(photo|video)-(\\d+)$")

fun parsePenRoutineQuestionProofFieldKey(fieldKey: String): PenRoutineQuestionProofKey? {
    val match = questionProofKeyPattern.matchEntire(fieldKey) ?: return null
    return PenRoutineQuestionProofKey(match.groupValues[1], match.groupValues[2], match.groupValues[3].toInt())
}

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PEN_ROUTINE_PRESENCE] —
 * the `enter` punch (`POST /app/pen-routines/{task}/presence`). The location and integrity
 * blocks are captured at TAP time and travel as recorded: the server judges them, the phone
 * never edits them on a retry.
 */
@Serializable
data class PenRoutinePresencePayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("event_type") val eventType: String,
    @SerialName("captured_at") val capturedAt: String,
    @SerialName("location") val location: PenRoutineLocationDto,
    @SerialName("integrity") val integrity: PenRoutineIntegrityDto,
)

/** One capture the submit carries, by REFERENCE to its PROOF_UPLOAD outbox row. */
@Serializable
data class PenRoutineSubmitProof(
    @SerialName("proof_outbox_item_id") val proofOutboxItemId: String,
    /** `photo` | `video`. */
    @SerialName("kind") val kind: String,
    /** The question this capture answers; blank for a task-wide capture. */
    @SerialName("question_id") val questionId: String = "",
)

/**
 * Outbox payload for [sg.mesha.goatos.core.database.outbox.OutboxOpType.PEN_ROUTINE_SUBMIT] —
 * the answers keyed by question id (already in the wire shape the server validates) plus every
 * capture by reference to its PROOF_UPLOAD row on the same task group, so the uploads drain
 * first and the dispatcher resolves the uploaded server proof ids. [rowVersion] is the task's
 * optimistic-concurrency token the screen last rendered.
 */
@Serializable
data class PenRoutineSubmitPayload(
    @SerialName("task_id") val taskId: String,
    @SerialName("row_version") val rowVersion: Int,
    @SerialName("answers") val answers: JsonObject,
    @SerialName("proofs") val proofs: List<PenRoutineSubmitProof>,
    @SerialName("captured_at") val capturedAt: String? = null,
    @SerialName("location") val location: PenRoutineLocationDto? = null,
    @SerialName("integrity") val integrity: PenRoutineIntegrityDto? = null,
)
