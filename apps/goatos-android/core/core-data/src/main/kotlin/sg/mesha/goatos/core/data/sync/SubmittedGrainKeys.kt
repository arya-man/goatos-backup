package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.network.dto.MilkPreparationFarmTaskDto
import sg.mesha.goatos.core.network.dto.MilkFeedingTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto

/**
 * The ONE definition of "which list row does this queued submit belong to".
 *
 * Both sides of the optimistic "In review" badge derive their key from HERE: the projection over
 * active outbox rows ([SyncRepository.observeSubmittedForReviewGrains]) and the list ViewModel that
 * looks a row up in that set. A second, independently-rebuilt key is exactly how the previous
 * design broke — it reconstructed the key at mark time and again at clear time, and the two
 * disagreed in three separate ways (packing `sessionNo` 0 vs its dispatched 1; a `businessDate()`
 * read at two different moments either side of midnight; blank vs null partition labels). Each
 * mismatch stranded the badge on "In review" for work the server had already refused.
 *
 * TWO RULES make that class of bug impossible here, and both must hold for every opType added later:
 *
 *  1. **No clock.** Every segment comes out of the payload. The date is the one the submit itself
 *     carries (target/feeding/preparation date), never "today" — so the key a row projects is the
 *     same key forever, whatever hour it is read at and whichever side of midnight it fails on.
 *  2. **Normalise once, here.** `sessionNo` 0 means "queued by the pen-day build" and dispatches as
 *     session 1 (see FeedPackingCompletePayload's kdoc), and a null/blank partition is the whole
 *     shed. Callers never repeat those rules; they call this.
 *
 * Returns null for an opType with no list badge — the caller skips it rather than inventing a key.
 */
internal fun submittedGrainKeyOf(opType: String, payloadJson: String, json: Json): String? {
    val type = OutboxOpTypeName.entries.firstOrNull { it.name == opType } ?: return null
    return try {
        decodeGrainKey(type, payloadJson, json)
    } catch (error: Exception) {
        // A payload that will not decode means this row's badge can never be projected. Never
        // silenced: the row would sit in the outbox rendering as un-submitted work with no clue why.
        android.util.Log.w("GoatOsOutbox", "submitted_grain_key_undecodable opType=$opType", error)
        null
    }
}

private fun decodeGrainKey(type: OutboxOpTypeName, payloadJson: String, json: Json): String {
    return when (type) {
        OutboxOpTypeName.FEED_PACKING_COMPLETE -> {
            val p = json.decodeFromString<FeedPackingCompletePayload>(payloadJson)
            shedSessionKey(p.targetDate, p.shedId, p.partitionLabel, normalizeSession(p.sessionNo), p.workflow)
        }
        OutboxOpTypeName.FEED_DIRECTION_COMPLETE -> {
            val p = json.decodeFromString<FeedDirectionCompletePayload>(payloadJson)
            shedSessionKey(p.targetDate, p.shedId, null, normalizeSession(p.sessionNo), p.workflow)
        }
        OutboxOpTypeName.FEED_DISTRIBUTION_COMPLETE -> {
            val p = json.decodeFromString<FeedDistributionCompletePayload>(payloadJson)
            shedSessionKey(p.targetDate, p.shedId, p.partitionLabel, normalizeSession(p.sessionNo), p.workflow)
        }
        OutboxOpTypeName.FEED_TRANSPORT_SUBMIT -> {
            val p = json.decodeFromString<FeedTransportSubmitPayload>(payloadJson)
            // Transport is task-grain and its payload carries no date; the task id is already unique.
            taskGrainKey("feed-transport", p.taskId)
        }
        OutboxOpTypeName.MILK_FEEDING_SUBMIT -> {
            val p = json.decodeFromString<MilkFeedingSubmitPayload>(payloadJson)
            taskGrainKey("milk-feeding", p.taskId)
        }
        OutboxOpTypeName.MILK_PREPARATION_SUBMIT -> {
            val p = json.decodeFromString<MilkPreparationSubmitPayload>(payloadJson)
            // Farm-day grain: one preparation per park per day.
            taskGrainKey("milk-preparation", p.parkId + "|" + p.preparationDate)
        }
    }
}

/** The opTypes that carry a list badge. Kept separate from OutboxOpType so adding an unrelated
 *  opType cannot silently change this projection. */
internal enum class OutboxOpTypeName {
    FEED_PACKING_COMPLETE,
    FEED_DIRECTION_COMPLETE,
    FEED_DISTRIBUTION_COMPLETE,
    FEED_TRANSPORT_SUBMIT,
    MILK_FEEDING_SUBMIT,
    MILK_PREPARATION_SUBMIT,
}

/** 0 means "queued by the pen-day build" and is dispatched as session 1 — normalise it ONCE. */
private fun normalizeSession(sessionNo: Int): Int = if (sessionNo < 1) 1 else sessionNo

/** Shed-session grain: the date the submit is FOR, never the date it is read on. */
fun shedSessionKey(
    date: String,
    shedId: String,
    partitionLabel: String?,
    sessionNo: Int,
    workflow: String,
): String = listOf(
    date,
    "shed-session",
    shedId,
    partitionLabel?.trim().orEmpty().lowercase().ifBlank { "whole" },
    sessionNo.toString(),
    workflow,
).joinToString("|")

/** Task grain (transport, milk): the id is already unique, so no date segment is needed. */
fun taskGrainKey(kind: String, taskId: String): String =
    listOf("task", kind, taskId).joinToString("|")

// ---- row-owned keys ---------------------------------------------------------------------------
//
// A list ViewModel must NEVER call [shedSessionKey]/[taskGrainKey] directly. Those take loose
// positional arguments, and passing the wrong one is invisible: the key simply never matches, the
// badge never appears, and every test and guard still passes. That is not hypothetical — the Feed
// Direction list once passed `null` for a partition its own submit sends as "1", silently
// reopening the 254.mp4 bug on every partitioned shed.
//
// So the ROW supplies its own key, straight off the DTO the list already renders. The only thing a
// caller can still pass is the date the row does not carry, and it is named in the signature. The
// wiring guard bans the raw builders inside ViewModels so this stays the only way in.

/**
 * [date] is the FEED day this packing row is for — what the worklist query sends as `targetDate`
 * (packing day + 1), which is the date the submit's payload carries.
 */
fun FeedPackingRowDto.submittedGrainKey(date: String): String =
    shedSessionKey(date, shedId, partitionLabel, sessionNo, workflow)

/**
 * [date] is this direction row's own target date.
 *
 * Tapping a direction row opens the DISTRIBUTION capture, which enqueues with THIS row's
 * partitionLabel — so the partition is taken from the row here, never assumed absent.
 */
fun FeedDirectionRowDto.submittedGrainKey(date: String): String =
    shedSessionKey(date, shedId, partitionLabel, sessionNo, workflow)

/** Task-grain rows carry their whole identity; nothing is left for a caller to supply. */
fun FeedTransportTaskDto.submittedGrainKey(): String = taskGrainKey("feed-transport", taskId)

fun MilkFeedingTaskDto.submittedGrainKey(): String = taskGrainKey("milk-feeding", taskId)

/** [preparationDate] is the day the list is showing; one preparation per park per day. */
fun MilkPreparationFarmTaskDto.submittedGrainKey(preparationDate: String): String =
    taskGrainKey("milk-preparation", parkId + "|" + preparationDate)
