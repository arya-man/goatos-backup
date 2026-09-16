package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * SOP CAPTURE CARD cache (maintainer decisions 4 and 7, 2026-09-16): the Add birth / Add death
 * form's published capture card, one JSON row per form kind (`birth` / `death`). Room is the
 * source the form renders from; the card is refreshed on open, so a publish reaches the phone on
 * its next refresh with no app update. Bounded by construction: two keys.
 */
@Entity(tableName = "counts_capture_card_cache", primaryKeys = ["scopeKey"])
data class CountsCaptureCardCacheEntity(
    val scopeKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface CountsCaptureCardCacheDao {
    @Query("SELECT * FROM counts_capture_card_cache WHERE scopeKey = :scopeKey")
    fun observe(scopeKey: String): Flow<CountsCaptureCardCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: CountsCaptureCardCacheEntity)
}

/**
 * A death workflow's DRAFT ANSWERS (death follows the SOP, 2026-09-16): an authored answer step is
 * answered on the phone and held here -- beside the recorded video/photo drafts -- until the ONE
 * Submit, so an answer survives process death exactly as a recorded clip does. One row per
 * (workflow, step); bounded by the SOP's step count.
 */
@Entity(tableName = "workflow_step_draft_answer", primaryKeys = ["workflowId", "actionId"])
data class WorkflowStepDraftAnswerEntity(
    val workflowId: String,
    val actionId: String,
    val answerValue: String,
    val updatedAt: Long,
)

@Dao
interface WorkflowStepDraftAnswerDao {
    // mobile-guard:ignore: bounded by one workflow's authored steps (a death SOP has a handful)
    @Query("SELECT * FROM workflow_step_draft_answer WHERE workflowId = :workflowId ORDER BY actionId LIMIT 64")
    fun observe(workflowId: String): Flow<List<WorkflowStepDraftAnswerEntity>>

    @Query("SELECT * FROM workflow_step_draft_answer WHERE workflowId = :workflowId ORDER BY actionId LIMIT 64")
    suspend fun list(workflowId: String): List<WorkflowStepDraftAnswerEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: WorkflowStepDraftAnswerEntity)

    @Query("DELETE FROM workflow_step_draft_answer WHERE workflowId = :workflowId")
    suspend fun clear(workflowId: String)
}
