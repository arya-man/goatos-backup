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
