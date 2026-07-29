package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus
import sg.mesha.goatos.feature.feed.FeedTransportUiState

@HiltViewModel class FeedTransportViewModel @Inject constructor(private val repo:FeedTransportRepository):ViewModel(){
    private val date=LocalDate.now(ZoneId.of("Asia/Kolkata")).toString();private val flags=MutableStateFlow(Triple(false,false,20))
    val state:StateFlow<FeedTransportUiState> = combine(repo.observe(date,100),flags){page,f->FeedTransportUiState(date,page.items.map{FeedTransportRowUi(it.taskId,it.parkId,it.shedId,it.shedLabel,it.parkLabel,it.status,it.reworkReason)},f.first,f.second,page.nextCursor!=null,false)}.stateIn(viewModelScope,SharingStarted.WhileSubscribed(5000),FeedTransportUiState(date=date))
    init{refresh()};fun onEvent(e:FeedTransportEvent){when(e){FeedTransportEvent.Refresh->refresh();FeedTransportEvent.LoadMore->viewModelScope.launch{repo.loadMore(date)};is FeedTransportEvent.Open->Unit}}
    private fun refresh()=viewModelScope.launch{flags.value=Triple(true,false,20);val r=repo.refresh(date);flags.value=Triple(false,r.isFailure,20)}
}

@HiltViewModel class FeedTransportCaptureViewModel @Inject constructor(private val sync:SyncRepository,private val capture:ProofCaptureSource,saved:SavedStateHandle):ViewModel(){
    private val taskId=saved.get<String>(ARG_TASK_ID).orEmpty();private val shedId=saved.get<String>(ARG_SHED_ID).orEmpty();private val shedLabel=saved.get<String>(ARG_SHED_LABEL).orEmpty();private val group="feed-transport:$taskId";private val proofKey=DraftIdempotencyKey(saved,"transport_proof_key","feed-transport-video");private val submitKey=DraftIdempotencyKey(saved,"transport_submit_key","feed-transport-submit");private val proofItem=DraftOutboxItemId(saved,"transport_proof_item");private val _state=MutableStateFlow(FeedTransportCaptureUiState(shedLabel=shedLabel,videoCaptured=proofItem.value!=null));val state:StateFlow<FeedTransportCaptureUiState> = _state
    fun onEvent(e:FeedTransportCaptureEvent){when(e){FeedTransportCaptureEvent.RecordVideo->record();FeedTransportCaptureEvent.Submit->submit();FeedTransportCaptureEvent.Back->Unit}}
    private fun record(){if(_state.value.isCapturing||_state.value.videoCaptured)return;_state.update{it.copy(isCapturing=true)};viewModelScope.launch{val v=capture.captureVideo(ProofCapturePrompt.FEED_TRANSPORT);if(v==null){_state.update{it.copy(isCapturing=false)};return@launch};val req=ProofUploadRequestDto(proofType="video",mimeType=v.mimeType,scopeType="shed",scopeId=shedId,subjectType="shed",subjectId=shedId,metadata=mapOf("capture_source" to JsonPrimitive(v.captureSource),"captured_start_ms" to JsonPrimitive(v.startedAtMs),"captured_end_ms" to JsonPrimitive(v.endedAtMs)));when(val r=sync.enqueueProofUpload(group,proofKey.current(),req,v.localUri,(v.endedAtMs-v.startedAtMs).takeIf{it>0})){is AppResult.Ok->{proofItem.value=r.value;_state.update{it.copy(isCapturing=false,videoCaptured=true,message="Video queued")}};is AppResult.Err->{proofKey.invalidate();_state.update{it.copy(isCapturing=false,message=r.message)}}}}}
    private fun submit(){val proof=proofItem.value?:return;viewModelScope.launch{when(val r=sync.enqueueFeedTransportSubmit(group,submitKey.current(),taskId,proof)){is AppResult.Ok->_state.update{it.copy(status=FeedTransportSubmitStatus.QUEUED,message="Verification due")};is AppResult.Err->{submitKey.invalidate();_state.update{it.copy(status=FeedTransportSubmitStatus.FAILED,message=r.message)}}}}}
    companion object{const val ARG_TASK_ID="task_id";const val ARG_SHED_ID="shed_id";const val ARG_SHED_LABEL="shed_label"}
}
