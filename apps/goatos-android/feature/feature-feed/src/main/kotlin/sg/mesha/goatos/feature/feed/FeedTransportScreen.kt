package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.RefreshOnResume

@Immutable data class FeedTransportRowUi(val taskId:String,val parkId:String,val shedId:String,val shedLabel:String,val parkLabel:String,val status:String,val reworkReason:String?)
@Immutable data class FeedTransportUiState(val date:String="",val rows:List<FeedTransportRowUi> = emptyList(),val isRefreshing:Boolean=false,val isOffline:Boolean=false,val hasMore:Boolean=false,val isLoadingMore:Boolean=false)
sealed interface FeedTransportEvent{data object Refresh:FeedTransportEvent;data object LoadMore:FeedTransportEvent;data class Open(val row:FeedTransportRowUi):FeedTransportEvent}

@Composable fun FeedTransportScreen(state:FeedTransportUiState,onEvent:(FeedTransportEvent)->Unit){RefreshOnResume{onEvent(FeedTransportEvent.Refresh)};Column(Modifier.fillMaxSize().background(MeshaColors.PageBg).padding(16.dp),verticalArrangement=Arrangement.spacedBy(12.dp)){Text("Feed Transport",fontSize=22.sp,fontWeight=FontWeight.Bold,color=MeshaColors.Ink);Text("Today · ${state.date} · Starts 15:30",color=MeshaColors.Muted);LazyColumn(verticalArrangement=Arrangement.spacedBy(10.dp)){items(state.rows,key={it.taskId}){row->Column(Modifier.fillMaxWidth().background(MeshaColors.Surf).clickable(enabled=row.status=="due"||row.status=="rework"){onEvent(FeedTransportEvent.Open(row))}.padding(14.dp)){Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceBetween){Text(row.shedLabel,fontWeight=FontWeight.Bold,color=MeshaColors.Ink);Text(row.status.replace('_',' '),color=MeshaColors.Muted)};Text(row.parkLabel,color=MeshaColors.Faint);row.reworkReason?.takeIf{it.isNotBlank()}?.let{Text("Rework: $it",color=MeshaColors.Danger)}}};if(state.hasMore){item{Button(enabled=!state.isLoadingMore,onClick={onEvent(FeedTransportEvent.LoadMore)}){Text("Load more")}}}}}}

enum class FeedTransportSubmitStatus{QUEUED,SYNCED,FAILED}
@Immutable data class FeedTransportCaptureUiState(val shedLabel:String="",val isCapturing:Boolean=false,val videoCaptured:Boolean=false,val message:String?=null,val status:FeedTransportSubmitStatus?=null)
sealed interface FeedTransportCaptureEvent{data object RecordVideo:FeedTransportCaptureEvent;data object Submit:FeedTransportCaptureEvent;data object Back:FeedTransportCaptureEvent}
@Composable fun FeedTransportCaptureScreen(state:FeedTransportCaptureUiState,onEvent:(FeedTransportCaptureEvent)->Unit){Column(Modifier.fillMaxSize().background(MeshaColors.PageBg).padding(16.dp),verticalArrangement=Arrangement.spacedBy(16.dp)){Text("Back",Modifier.clickable{onEvent(FeedTransportCaptureEvent.Back)},color=MeshaColors.Muted);Text(state.shedLabel,fontSize=22.sp,fontWeight=FontWeight.Bold,color=MeshaColors.Ink);Text("Record one fresh live-camera video of feed transport for this shed.",color=MeshaColors.Muted);Button(enabled=!state.isCapturing&&!state.videoCaptured&&state.status==null,onClick={onEvent(FeedTransportCaptureEvent.RecordVideo)}){Text(if(state.isCapturing)"Recording…" else "Record live video")};if(state.videoCaptured)Text("Video ready",color=MeshaColors.Ok);Button(enabled=state.videoCaptured&&state.status==null,onClick={onEvent(FeedTransportCaptureEvent.Submit)}){Text("Done")};state.message?.let{Text(it,color=MeshaColors.Muted)}}}
