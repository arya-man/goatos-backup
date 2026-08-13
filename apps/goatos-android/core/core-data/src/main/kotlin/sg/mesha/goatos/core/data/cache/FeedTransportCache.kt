package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Entity(tableName="feed_transport_items",indices=[Index(value=["businessDate","sortIndex"])])
data class FeedTransportItemEntity(@PrimaryKey val taskId:String,val businessDate:String,val sortIndex:Int,val dtoJson:String,val updatedAt:Long)
@Entity(tableName="feed_transport_remote_keys")
data class FeedTransportRemoteKeyEntity(@PrimaryKey val businessDate:String,val nextCursor:String?,val endReached:Boolean,val updatedAt:Long)

@Entity(
    tableName = "feed_transport_scoped_items",
    primaryKeys = ["scopeKey", "taskId"],
    indices = [Index(value = ["scopeKey", "sortIndex"])],
)
data class FeedTransportScopedItemEntity(
    val scopeKey: String,
    val taskId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "feed_transport_scoped_remote_keys")
data class FeedTransportScopedRemoteKeyEntity(
    @PrimaryKey val scopeKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val filtersJson: String,
    val updatedAt: Long,
)

@Dao interface FeedTransportItemDao{
    @Query("SELECT * FROM feed_transport_items WHERE businessDate=:date ORDER BY sortIndex,taskId LIMIT :limit") fun observe(date:String,limit:Int):Flow<List<FeedTransportItemEntity>>
    @Insert(onConflict=OnConflictStrategy.REPLACE) suspend fun upsertAll(items:List<FeedTransportItemEntity>)
    @Query("DELETE FROM feed_transport_items WHERE businessDate=:date") suspend fun deleteDate(date:String)
    @Query("SELECT COUNT(*) FROM feed_transport_items WHERE businessDate=:date") suspend fun count(date:String):Int
}
@Dao interface FeedTransportRemoteKeyDao{
    @Query("SELECT * FROM feed_transport_remote_keys WHERE businessDate=:date") fun observe(date:String):Flow<FeedTransportRemoteKeyEntity?>
    @Query("SELECT * FROM feed_transport_remote_keys WHERE businessDate=:date") suspend fun get(date:String):FeedTransportRemoteKeyEntity?
    @Insert(onConflict=OnConflictStrategy.REPLACE) suspend fun upsert(key:FeedTransportRemoteKeyEntity)
}

@Dao
interface FeedTransportScopedItemDao {
    @Query("SELECT * FROM feed_transport_scoped_items WHERE scopeKey=:scopeKey ORDER BY sortIndex,taskId LIMIT :limit")
    fun observe(scopeKey: String, limit: Int): Flow<List<FeedTransportScopedItemEntity>>

    /**
     * The MOST RECENTLY cached row for one transport task, across ANY filter scope — the task list
     * screen may have paged it under a different park/shed/status filter than whichever filter is
     * active when the capture screen opens. Used to observe the task's live `status` (submitted /
     * pending_verification / etc.) from the same Room table the list renders from.
     */
    @Query("SELECT * FROM feed_transport_scoped_items WHERE taskId=:taskId ORDER BY updatedAt DESC LIMIT 1")
    fun observeByTaskId(taskId: String): Flow<FeedTransportScopedItemEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<FeedTransportScopedItemEntity>)

    @Query("DELETE FROM feed_transport_scoped_items WHERE scopeKey=:scopeKey")
    suspend fun deleteScope(scopeKey: String)

    /**
     * Drops rows cached under the retired pen-grain scope key, which carried a partition segment
     * ("date|park|shed|partition|status") that today's key does not. Nothing reads them again, so
     * without this they would sit in the database forever.
     */
    @Query("DELETE FROM feed_transport_scoped_items WHERE scopeKey LIKE '%|%|%|%|%'")
    suspend fun deleteLegacyPartitionScopes()

    @Query("SELECT COUNT(*) FROM feed_transport_scoped_items WHERE scopeKey=:scopeKey")
    suspend fun count(scopeKey: String): Int
}

@Dao
interface FeedTransportScopedRemoteKeyDao {
    @Query("SELECT * FROM feed_transport_scoped_remote_keys WHERE scopeKey=:scopeKey")
    fun observe(scopeKey: String): Flow<FeedTransportScopedRemoteKeyEntity?>

    @Query("SELECT * FROM feed_transport_scoped_remote_keys WHERE scopeKey=:scopeKey")
    suspend fun get(scopeKey: String): FeedTransportScopedRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: FeedTransportScopedRemoteKeyEntity)

    /** Companion of [FeedTransportScopedItemDao.deleteLegacyPartitionScopes]. */
    @Query("DELETE FROM feed_transport_scoped_remote_keys WHERE scopeKey LIKE '%|%|%|%|%'")
    suspend fun deleteLegacyPartitionScopes()
}
