package sg.mesha.goatos.core.data

import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.combine
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.FeedTransportItemEntity
import sg.mesha.goatos.core.data.cache.FeedTransportRemoteKeyEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto

class FeedTransportRepository(private val api:AppApi,private val db:GoatDatabase,private val json:Json=Json{ignoreUnknownKeys=true},private val clock:()->Long={System.currentTimeMillis()}){
    /**
     * Cache-first page Flow. The per-row `dtoJson` decode is CPU work over the whole visible
     * window and Room emits on its query executor, so the mapping is moved off the collector's
     * dispatcher with [flowOn] — the collector is the UI, and decoding a window of rows on Main
     * on every Room emission drops frames. [flowOn] is upstream-only: it does not change where
     * the ViewModel/UI collects.
     */
    fun observe(date:String,limit:Int):Flow<FeedTransportTaskPageDto> = combine(db.feedTransportItemDao().observe(date,limit),db.feedTransportRemoteKeyDao().observe(date)){rows,key->FeedTransportTaskPageDto(rows.map{json.decodeFromString<FeedTransportTaskDto>(it.dtoJson)},key?.nextCursor)}.flowOn(Dispatchers.Default)
    suspend fun refresh(date:String):Result<Unit> = runCatching{val page=api.getFeedTransportTasks(date,null,20);val now=clock();db.withTransaction{db.feedTransportItemDao().deleteDate(date);db.feedTransportItemDao().upsertAll(page.items.mapIndexed{i,x->FeedTransportItemEntity(x.taskId,date,i,json.encodeToString(x),now)});db.feedTransportRemoteKeyDao().upsert(FeedTransportRemoteKeyEntity(date,page.nextCursor,page.nextCursor==null,now))}}
    suspend fun loadMore(date:String):Result<Unit> = runCatching{val key=db.feedTransportRemoteKeyDao().get(date)?:return@runCatching;val cursor=key.nextCursor?:return@runCatching;val page=api.getFeedTransportTasks(date,cursor,20);val now=clock();db.withTransaction{val base=db.feedTransportItemDao().count(date);db.feedTransportItemDao().upsertAll(page.items.mapIndexed{i,x->FeedTransportItemEntity(x.taskId,date,base+i,json.encodeToString(x),now)});db.feedTransportRemoteKeyDao().upsert(FeedTransportRemoteKeyEntity(date,page.nextCursor,page.nextCursor==null,now))}}
}
