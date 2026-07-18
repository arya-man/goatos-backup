package sg.mesha.goatos.core.data.cache

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query

/** One independently pageable Calendar row. Pages never accumulate in ViewModel memory. */
@Entity(
    tableName = "calendar_schedule_items",
    primaryKeys = ["queryKey", "eventId"],
    indices = [Index(value = ["queryKey", "dueAt", "eventId"])],
)
data class CalendarScheduleEntity(
    val queryKey: String,
    val eventId: String,
    val dueAt: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The opaque backend keyset cursor for the next 20-row page of one filter scope. */
@Entity(tableName = "calendar_schedule_remote_keys")
data class CalendarScheduleRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val updatedAt: Long,
)

@Dao
interface CalendarScheduleDao {
    @Query(
        "SELECT * FROM calendar_schedule_items WHERE queryKey = :queryKey " +
            "ORDER BY dueAt ASC, eventId ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, CalendarScheduleEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<CalendarScheduleEntity>)

    @Query("DELETE FROM calendar_schedule_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    @Query(
        "DELETE FROM calendar_schedule_items WHERE queryKey IN " +
            "(SELECT queryKey FROM calendar_schedule_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface CalendarScheduleRemoteKeyDao {
    @Query("SELECT * FROM calendar_schedule_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): CalendarScheduleRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: CalendarScheduleRemoteKeyEntity)

    @Query("DELETE FROM calendar_schedule_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM calendar_schedule_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM calendar_schedule_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
