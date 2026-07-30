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

@Entity(
    tableName = "health_work_items",
    primaryKeys = ["scopeKey", "healthSessionId"],
    indices = [Index(value = ["scopeKey", "sortIndex"])],
)
data class HealthWorkItemEntity(
    val scopeKey: String,
    val healthSessionId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "health_remote_keys")
data class HealthRemoteKeyEntity(
    @PrimaryKey val scopeKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Entity(tableName = "health_page_meta")
data class HealthPageMetaEntity(
    @PrimaryKey val scopeKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "health_work_item_details")
data class HealthWorkItemDetailEntity(
    @PrimaryKey val healthSessionId: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface HealthWorkItemDao {
    @Query(
        "SELECT * FROM health_work_items WHERE scopeKey = :scopeKey " +
            "ORDER BY sortIndex ASC, healthSessionId ASC",
    )
    fun pagingSource(scopeKey: String): PagingSource<Int, HealthWorkItemEntity>

    @Query("SELECT COUNT(*) FROM health_work_items WHERE scopeKey = :scopeKey")
    suspend fun count(scopeKey: String): Int

    @Query("SELECT * FROM health_work_items WHERE healthSessionId = :healthSessionId LIMIT 12")
    suspend fun findAll(healthSessionId: String): List<HealthWorkItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<HealthWorkItemEntity>)

    @Query("DELETE FROM health_work_items WHERE scopeKey = :scopeKey")
    suspend fun deleteScope(scopeKey: String)

    @Query(
        "DELETE FROM health_work_items WHERE scopeKey IN " +
            "(SELECT scopeKey FROM health_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepScopes)",
    )
    suspend fun deleteOutsideNewestScopes(keepScopes: Int)
}

@Dao
interface HealthRemoteKeyDao {
    @Query("SELECT * FROM health_remote_keys WHERE scopeKey = :scopeKey")
    suspend fun get(scopeKey: String): HealthRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: HealthRemoteKeyEntity)

    @Query("DELETE FROM health_remote_keys WHERE scopeKey = :scopeKey")
    suspend fun delete(scopeKey: String)

    @Query(
        "DELETE FROM health_remote_keys WHERE scopeKey IN " +
            "(SELECT scopeKey FROM health_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepScopes)",
    )
    suspend fun deleteOutsideNewestScopes(keepScopes: Int)
}

@Dao
interface HealthPageMetaDao {
    @Query("SELECT * FROM health_page_meta WHERE scopeKey = :scopeKey")
    fun observe(scopeKey: String): Flow<HealthPageMetaEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: HealthPageMetaEntity)

    @Query("DELETE FROM health_page_meta WHERE scopeKey = :scopeKey")
    suspend fun delete(scopeKey: String)
}

@Dao
interface HealthWorkItemDetailDao {
    @Query("SELECT * FROM health_work_item_details WHERE healthSessionId = :healthSessionId")
    fun observe(healthSessionId: String): Flow<HealthWorkItemDetailEntity?>

    @Query("SELECT * FROM health_work_item_details WHERE healthSessionId = :healthSessionId")
    suspend fun get(healthSessionId: String): HealthWorkItemDetailEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: HealthWorkItemDetailEntity)

    @Query("DELETE FROM health_work_item_details WHERE healthSessionId = :healthSessionId")
    suspend fun delete(healthSessionId: String)

    @Query(
        "DELETE FROM health_work_item_details WHERE healthSessionId IN " +
            "(SELECT healthSessionId FROM health_work_item_details ORDER BY updatedAt DESC LIMIT -1 OFFSET :keep)",
    )
    suspend fun deleteOldestBeyond(keep: Int)
}
