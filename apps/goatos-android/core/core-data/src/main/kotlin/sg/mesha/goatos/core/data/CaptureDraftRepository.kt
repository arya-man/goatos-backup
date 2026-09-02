package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.CaptureEvidenceDraftEntity
import sg.mesha.goatos.core.data.cache.CaptureEvidenceDraftEntity.Companion.ANSWERS_STEP
import sg.mesha.goatos.core.data.cache.CaptureEvidenceDraftEntity.Companion.SUBMIT_STEP

/** The capture flows that keep durable drafts. One key per work-item family. */
object CaptureFlow {
    const val SHIFTING = "shifting"
    const val PEN_RECONCILIATION = "pen_reconciliation"
    const val FEED_DISTRIBUTION = "feed_distribution"
    const val FEED_PACKING = "feed_packing"
    const val FEED_WASTAGE = "feed_wastage"
    const val FEED_TRANSPORT = "feed_transport"
    const val MILK_PREPARATION = "milk_preparation"
    const val MILK_FEEDING = "milk_feeding"
    const val PC_CARE = "pc_care"
    const val HEALTH_TREATMENT = "health_treatment"
}

/**
 * One work item's captured evidence and submit handles, as the screen sees it.
 *
 * [proofs] maps the flow's step name to the PROOF_UPLOAD outbox item id already enqueued for it, so
 * a re-entered screen renders "recorded" instead of asking for the clip again.
 */
data class CaptureDraft(
    val proofs: Map<String, String> = emptyMap(),
    val fingerprint: String? = null,
    val submitIdempotencyKey: String? = null,
    val submitOutboxItemId: String? = null,
    /** The operator's typed form answers, field key -> value, as last saved by the screen. */
    val answers: Map<String, String> = emptyMap(),
) {
    val capturedCount: Int get() = proofs.size

    fun hasProof(step: String): Boolean = proofs.containsKey(step)

    /** One typed answer, or empty when the operator has not filled that field yet. */
    fun answer(field: String): String = answers[field].orEmpty()
}

/**
 * Durable, navigation-independent storage for in-progress capture work.
 *
 * Every capture screen (shifting execution, feed distribution/packing/transport, milk
 * preparation/feeding) writes its recorded proofs here BEFORE flipping its UI, so leaving and
 * re-entering a work item keeps the evidence. See [CaptureEvidenceDraftEntity] for why
 * `SavedStateHandle` could not do this job.
 */
interface CaptureDraftRepository {
    suspend fun find(flowKey: String, entityId: String): CaptureDraft

    fun observe(flowKey: String, entityId: String): Flow<CaptureDraft>

    /**
     * Per-work-item captured-proof counts for one flow, bounded by [limit] — what a work list uses to
     * show how far each task has got.
     */
    fun observeProgress(flowKey: String, limit: Int = CAPTURE_PROGRESS_WINDOW): Flow<Map<String, Int>>

    /** Records (or replaces) one step's proof. */
    suspend fun putProof(flowKey: String, entityId: String, step: String, outboxItemId: String, fingerprint: String? = null)

    /** Drops one step's proof — a re-record about to replace it, or invalidated evidence. */
    suspend fun clearProof(flowKey: String, entityId: String, step: String)

    /** Stores the submit's stable idempotency key and, once enqueued, its outbox item id. */
    suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?)

    /**
     * Replaces this work item's typed form answers. The whole answer set is written at once (one row),
     * so a field cleared by the operator is a field cleared in the draft.
     */
    suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>)

    /** Drops every row for this work item: accepted, cancelled, or reset for rework. */
    suspend fun clear(flowKey: String, entityId: String)
}

/** Three screen-pages' worth of in-progress items — more than one operator can be mid-way through. */
const val CAPTURE_PROGRESS_WINDOW = 60

class DefaultCaptureDraftRepository(
    private val database: GoatDatabase,
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CaptureDraftRepository {
    private val dao get() = database.captureEvidenceDraftDao()

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft =
        dao.findFor(flowKey, entityId).toDraft()

    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> =
        dao.observeFor(flowKey, entityId)
            .map { it.toDraft() }
            .flowOn(Dispatchers.Default)

    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> =
        dao.observeProgress(flowKey, limit)
            .map { rows -> rows.associate { it.entityId to it.capturedCount } }
            .flowOn(Dispatchers.Default)

    override suspend fun putProof(
        flowKey: String,
        entityId: String,
        step: String,
        outboxItemId: String,
        fingerprint: String?,
    ) {
        dao.upsert(
            CaptureEvidenceDraftEntity(
                flowKey = flowKey,
                entityId = entityId,
                step = step,
                outboxItemId = outboxItemId,
                fingerprint = fingerprint,
                updatedAt = clock(),
            ),
        )
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        dao.deleteStep(flowKey, entityId, step)
    }

    override suspend fun putSubmit(
        flowKey: String,
        entityId: String,
        idempotencyKey: String?,
        outboxItemId: String?,
    ) {
        dao.upsert(
            CaptureEvidenceDraftEntity(
                flowKey = flowKey,
                entityId = entityId,
                step = SUBMIT_STEP,
                outboxItemId = outboxItemId,
                idempotencyKey = idempotencyKey,
                updatedAt = clock(),
            ),
        )
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        dao.upsert(
            CaptureEvidenceDraftEntity(
                flowKey = flowKey,
                entityId = entityId,
                step = ANSWERS_STEP,
                answers = encodeAnswers(answers),
                updatedAt = clock(),
            ),
        )
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        dao.deleteFor(flowKey, entityId)
    }

    private fun List<CaptureEvidenceDraftEntity>.toDraft(): CaptureDraft {
        val submit = firstOrNull { it.step == SUBMIT_STEP }
        val answers = firstOrNull { it.step == ANSWERS_STEP }
        val proofRows = filter { it.step != SUBMIT_STEP && it.step != ANSWERS_STEP && !it.outboxItemId.isNullOrBlank() }
        return CaptureDraft(
            proofs = proofRows.associate { it.step to it.outboxItemId.orEmpty() },
            fingerprint = proofRows.firstNotNullOfOrNull { it.fingerprint },
            submitIdempotencyKey = submit?.idempotencyKey,
            submitOutboxItemId = submit?.outboxItemId,
            answers = decodeAnswers(answers?.answers),
        )
    }
}

private val answersJson = Json { ignoreUnknownKeys = true }

private fun encodeAnswers(answers: Map<String, String>): String =
    answersJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), answers)

/**
 * A draft is a convenience, never a correctness input: a row written by an older build, hand-edited,
 * or truncated must not crash the screen it is restoring, so an undecodable blob degrades to "no
 * answers saved yet" rather than propagating.
 */
private fun decodeAnswers(raw: String?): Map<String, String> {
    if (raw.isNullOrBlank()) return emptyMap()
    return runCatching {
        answersJson.decodeFromString(MapSerializer(String.serializer(), String.serializer()), raw)
    }.getOrDefault(emptyMap())
}
