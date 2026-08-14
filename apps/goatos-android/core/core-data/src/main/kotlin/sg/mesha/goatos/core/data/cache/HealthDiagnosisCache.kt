package sg.mesha.goatos.core.data.cache

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * One assessment the register produced for one animal, held on the phone.
 *
 * Room is the SSOT for this read (docs/decisions/android-offline-first.md): the
 * proposal is looked at in a shed, where the network is worst, and a manager who
 * has just recorded an observation must be able to re-open what came back
 * without waiting for a request that may not complete.
 *
 * The whole proposal is kept as its wire JSON rather than exploded into columns.
 * It is a read-only document the phone never edits, its shape is owned by the
 * register version pinned inside it, and splitting it into columns would mean a
 * Room migration every time the register learns a new field.
 *
 * [status] is promoted out of the blob so the screen can tell an assessment still
 * awaiting a decision from one already decided without parsing it.
 */
@Entity(
    tableName = "health_diagnosis_runs",
    // Every assessment for one animal, for the animal's own history.
    indices = [Index(value = ["goatId", "observedAtMs"])],
)
data class HealthDiagnosisRunEntity(
    @PrimaryKey val diagnosisRunId: String,
    val goatId: String,
    /** The animal as a person recognises it, so the queue does not render a uuid. */
    val goatDisplayId: String,
    /** `proposed`, `confirmed` or `declined`. */
    val status: String,
    /** Sort key for both indices; epoch millis of the observation, not of the sync. */
    val observedAtMs: Long,
    /** The proposal exactly as the server sent it. */
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface HealthDiagnosisRunDao {
    @Query("SELECT * FROM health_diagnosis_runs WHERE diagnosisRunId = :diagnosisRunId")
    fun observe(diagnosisRunId: String): Flow<HealthDiagnosisRunEntity?>

    @Query("SELECT * FROM health_diagnosis_runs WHERE diagnosisRunId = :diagnosisRunId")
    suspend fun get(diagnosisRunId: String): HealthDiagnosisRunEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: HealthDiagnosisRunEntity)

    @Query("DELETE FROM health_diagnosis_runs WHERE diagnosisRunId = :diagnosisRunId")
    suspend fun delete(diagnosisRunId: String)

    /**
     * Keeps the cache bounded. A decided run is history the server owns; the phone
     * holds only enough of it to render what the manager just did.
     */
    @Query(
        "DELETE FROM health_diagnosis_runs WHERE diagnosisRunId IN " +
            "(SELECT diagnosisRunId FROM health_diagnosis_runs " +
            "ORDER BY updatedAt DESC LIMIT -1 OFFSET :keep)",
    )
    suspend fun deleteOldestBeyond(keep: Int)
}

/**
 * One row of the Director's queue, as a PAGED list.
 *
 * Deliberately a SEPARATE table from [HealthDiagnosisRunEntity]. That one is the
 * detail cache — the whole proposal for one animal, fetched when a screen opens
 * it. This one is a bounded keyset window of queue rows, replaced wholesale when
 * the queue refreshes. Mixing a paged list and a detail cache in one table means
 * a refresh of one silently evicts the other.
 *
 * [sortIndex] is the BACKEND's page order, not a local sort. The queue is ranked
 * newest-first server-side and clients must not re-derive it.
 */
@Entity(
    tableName = "health_diagnosis_queue_items",
    primaryKeys = ["scopeKey", "diagnosisRunId"],
    indices = [Index(value = ["scopeKey", "sortIndex"])],
)
data class HealthDiagnosisQueueItemEntity(
    val scopeKey: String,
    val diagnosisRunId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/**
 * The keyset cursor for one queue scope.
 *
 * [nextCursor] is opaque and server-issued. Storing it rather than an offset is
 * what lets a resumed scroll continue from where it was without re-showing rows
 * that new observations pushed down.
 */
@Entity(tableName = "health_diagnosis_queue_keys")
data class HealthDiagnosisQueueKeyEntity(
    @PrimaryKey val scopeKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    /** Whether this device's user may decide. Server-owned; cached so the list renders offline. */
    val mayConfirm: Boolean,
    val updatedAt: Long,
)

@Dao
interface HealthDiagnosisQueueDao {
    @Query(
        "SELECT * FROM health_diagnosis_queue_items WHERE scopeKey = :scopeKey " +
            "ORDER BY sortIndex ASC, diagnosisRunId ASC",
    )
    fun pagingSource(scopeKey: String): PagingSource<Int, HealthDiagnosisQueueItemEntity>

    @Query("SELECT COUNT(*) FROM health_diagnosis_queue_items WHERE scopeKey = :scopeKey")
    suspend fun count(scopeKey: String): Int

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<HealthDiagnosisQueueItemEntity>)

    @Query("DELETE FROM health_diagnosis_queue_items WHERE scopeKey = :scopeKey")
    suspend fun deleteScope(scopeKey: String)

    /** Keeps the cache bounded to the few scopes a person actually switches between. */
    @Query(
        "DELETE FROM health_diagnosis_queue_items WHERE scopeKey IN " +
            "(SELECT scopeKey FROM health_diagnosis_queue_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepScopes)",
    )
    suspend fun deleteOutsideNewestScopes(keepScopes: Int)
}

@Dao
interface HealthDiagnosisQueueKeyDao {
    @Query("SELECT * FROM health_diagnosis_queue_keys WHERE scopeKey = :scopeKey")
    suspend fun get(scopeKey: String): HealthDiagnosisQueueKeyEntity?

    @Query("SELECT * FROM health_diagnosis_queue_keys WHERE scopeKey = :scopeKey")
    fun observe(scopeKey: String): Flow<HealthDiagnosisQueueKeyEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: HealthDiagnosisQueueKeyEntity)

    @Query("DELETE FROM health_diagnosis_queue_keys WHERE scopeKey = :scopeKey")
    suspend fun delete(scopeKey: String)

    @Query(
        "DELETE FROM health_diagnosis_queue_keys WHERE scopeKey IN " +
            "(SELECT scopeKey FROM health_diagnosis_queue_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepScopes)",
    )
    suspend fun deleteOutsideNewestScopes(keepScopes: Int)
}
