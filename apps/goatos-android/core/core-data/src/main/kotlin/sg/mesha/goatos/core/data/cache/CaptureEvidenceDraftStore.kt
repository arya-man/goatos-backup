package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

/**
 * One captured proof (or one pending submit) belonging to a work item the operator is mid-way
 * through, durable across navigation.
 *
 * ### The bug class this exists to close
 *
 * Every capture screen used to hold its draft handles — the recorded proofs' outbox item ids and the
 * submit's idempotency key — in `SavedStateHandle`, which lives and dies with the nav backstack
 * entry. Pressing Back popped the destination, so re-entering the SAME work item built a fresh
 * ViewModel that had forgotten the video the operator had already recorded: the screen demanded a
 * re-record while the first clip uploaded anyway (maintainer reports 2026-07-30, first on Shifting
 * and then again on Feed Direction). The same loss hit the submit idempotency key, which is the
 * more dangerous half — a re-minted key means the server cannot collapse a resend onto the write it
 * already committed.
 *
 * It is deliberately GENERIC rather than one table per module: the defect was identical in shifting,
 * feed distribution, feed packing, feed transport, milk preparation, and milk feeding, so the fix is
 * one store every capture flow shares.
 *
 * Grain: one row per ([flowKey], [entityId], [step]). [step] is the flow's own proof step
 * ("shifting", "packing", "feeding", "video", "water", …), [SUBMIT_STEP] for the submit handles, or
 * [ANSWERS_STEP] for the operator's typed form answers. Rows for a work item are deleted once its
 * submit is accepted, so the table holds only work in progress.
 *
 * ### Why typed answers live here too
 *
 * Closing the proof half of the bug left the other half open: the videos came back on re-entry but
 * the numbers, remarks, and watchlist answers beside them did not, because those stayed in
 * `SavedStateHandle` (milk preparation) or plain in-memory state (milk feeding). Milk preparation
 * gates each step on `answerComplete && captured`, so an operator returning to the screen saw their
 * clips intact and every field blank, and had to retype the whole sheet before Submit re-enabled.
 * Answers are therefore written to the same durable row family as the proofs they belong to
 * (maintainer report 2026-07-31).
 */
@Entity(tableName = "capture_evidence_drafts", primaryKeys = ["flowKey", "entityId", "step"])
data class CaptureEvidenceDraftEntity(
    /** The capture flow, e.g. `shifting` or `feed_distribution`. */
    val flowKey: String,
    /** The work item within that flow — movement id, shed-session key, task id. */
    val entityId: String,
    /** The proof step, or [SUBMIT_STEP] for the row carrying the submit handles. */
    val step: String,
    /** PROOF_UPLOAD outbox item id for a proof step; the submit's outbox item id on [SUBMIT_STEP]. */
    val outboxItemId: String? = null,
    /** The submit's stable idempotency key (only meaningful on [SUBMIT_STEP]). */
    val idempotencyKey: String? = null,
    /** Config fingerprint the proof was captured against, where the flow has one. */
    val fingerprint: String? = null,
    /**
     * The operator's typed form answers as a JSON object of field key -> value (only meaningful on
     * [ANSWERS_STEP]). Opaque to this store: each flow owns its own field vocabulary.
     */
    val answers: String? = null,
    val updatedAt: Long,
) {
    companion object {
        /** The reserved [step] holding a work item's submit handles rather than a proof. */
        const val SUBMIT_STEP = "__submit__"

        /** The reserved [step] holding a work item's typed form answers rather than a proof. */
        const val ANSWERS_STEP = "__answers__"
    }
}

/** How many proofs one work item already has, for a list's per-task progress. */
data class CaptureEvidenceProgressRow(
    val entityId: String,
    val capturedCount: Int,
)

@Dao
interface CaptureEvidenceDraftDao {
    /** Every draft row for ONE work item — bounded by that item's step count. */
    @Query("SELECT * FROM capture_evidence_drafts WHERE flowKey = :flowKey AND entityId = :entityId")
    suspend fun findFor(flowKey: String, entityId: String): List<CaptureEvidenceDraftEntity>

    /** Observes ONE work item's drafts so a screen re-renders when its own capture lands. */
    @Query("SELECT * FROM capture_evidence_drafts WHERE flowKey = :flowKey AND entityId = :entityId")
    fun observeFor(flowKey: String, entityId: String): Flow<List<CaptureEvidenceDraftEntity>>

    /**
     * Per-work-item captured-proof counts for one flow, most recently touched first and bounded by
     * [limit] — the window that decorates a work list with progress. Deliberately NOT an unbounded
     * table read (docs/decisions/mobile-data-fetch-anti-patterns.md).
     *
     * projection-review: producer writes one row per (flowKey, entityId, step) — the entity's primary
     * key, so (flowKey, entityId, step) is unique. This consumer matches on flowKey and groups by
     * entityId, counting rows 1:1 with steps; there is no join, so no fan-out is possible. The two
     * reserved steps are excluded because neither is a proof: `__submit__` carries the submit handles
     * and `__answers__` the typed form answers. `outboxItemId IS NOT NULL` alone would exclude both
     * today, but the count must stay "captured proofs" even if a reserved row later gains an outbox
     * id, so the steps are named explicitly.
     */
    @Query(
        "SELECT entityId, COUNT(*) AS capturedCount FROM capture_evidence_drafts " +
            "WHERE flowKey = :flowKey AND step NOT IN ('__submit__', '__answers__') " +
            "AND outboxItemId IS NOT NULL " +
            "GROUP BY entityId ORDER BY MAX(updatedAt) DESC LIMIT :limit",
    )
    fun observeProgress(flowKey: String, limit: Int): Flow<List<CaptureEvidenceProgressRow>>

    @Upsert
    suspend fun upsert(entity: CaptureEvidenceDraftEntity)

    /** Drops ONE step, e.g. when a re-record replaces the clip that step held. */
    @Query("DELETE FROM capture_evidence_drafts WHERE flowKey = :flowKey AND entityId = :entityId AND step = :step")
    suspend fun deleteStep(flowKey: String, entityId: String, step: String)

    /** Drops every row for a work item — its submit was accepted, or its evidence was invalidated. */
    @Query("DELETE FROM capture_evidence_drafts WHERE flowKey = :flowKey AND entityId = :entityId")
    suspend fun deleteFor(flowKey: String, entityId: String)
}
