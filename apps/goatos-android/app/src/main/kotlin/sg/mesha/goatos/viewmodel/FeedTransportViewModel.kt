package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.flow.updateAndGet
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus
import sg.mesha.goatos.feature.feed.FeedTransportUiState

private data class FeedTransportFlags(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val isLoadingMore: Boolean = false,
    /** Sticky: set once a LoadMore fails to grow the rendered window. Kills the spin. */
    val endReached: Boolean = false,
)

/** One page of the transport list. ~20 rows per the repo's mobile list-fetch rule. */
private const val TRANSPORT_PAGE_SIZE = 20

@HiltViewModel
class FeedTransportViewModel @Inject constructor(
    private val repo: FeedTransportRepository,
) : ViewModel() {
    private val date = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    private val flags = MutableStateFlow(FeedTransportFlags())

    // The visible window over the Room-backed SSOT. Starts at ONE page and grows on LoadMore —
    // the DAO query is re-run at the new size (ScanViewModel's `_windowSize` pattern). Before
    // this, the DAO window was a frozen `limit = 100` while `hasMore` was derived from the
    // remote cursor, so on any day with >20 rows LoadMore could fire forever against a list
    // that never changed: the rows the UI rendered could not grow, `hasMore` never went false,
    // and the list kept re-requesting — an unbounded loop (ANR risk).
    private val window = MutableStateFlow(TRANSPORT_PAGE_SIZE)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedPage: StateFlow<FeedTransportTaskPageDto> =
        window.flatMapLatest { size -> repo.observe(date, size) }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), FeedTransportTaskPageDto(emptyList(), null))

    val state: StateFlow<FeedTransportUiState> = combine(observedPage, flags, window) { page, current, size ->
        FeedTransportUiState(
            date = date,
            rows = page.items.map {
                FeedTransportRowUi(it.taskId, it.parkId, it.shedId, it.shedLabel, it.parkLabel, it.status, it.reworkReason)
            },
            isRefreshing = current.isRefreshing,
            isOffline = current.isOffline,
            // Only offer more when the window is actually FULL (so growing it can yield rows) and
            // a previous LoadMore has not already proven there is nothing left. Never derived
            // from the remote cursor alone — that is what made the loop unbounded.
            hasMore = !current.endReached && page.items.size >= size,
            isLoadingMore = current.isLoadingMore,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), FeedTransportUiState(date = date))

    init {
        refresh()
    }

    fun onEvent(event: FeedTransportEvent) {
        when (event) {
            FeedTransportEvent.Refresh -> refresh()
            FeedTransportEvent.LoadMore -> loadMore()
            is FeedTransportEvent.Open -> Unit
        }
    }

    private fun refresh() = viewModelScope.launch {
        flags.value = flags.value.copy(isRefreshing = true, isOffline = false)
        val result = repo.refresh(date)
        // A refresh replaces the day's rows, so a previous "nothing left" verdict no longer holds
        // and the window goes back to one page — the same reset a fresh screen entry would give.
        window.value = TRANSPORT_PAGE_SIZE
        flags.value = flags.value.copy(isRefreshing = false, isOffline = result.isFailure, endReached = false)
    }

    /**
     * Grow the rendered window by one page, pulling the next network page first when the local
     * cache is already exhausted. Terminates: if the window grows and the row count does NOT
     * increase, there is nothing left and [FeedTransportFlags.endReached] latches `hasMore` off.
     */
    private fun loadMore() {
        val current = flags.value
        if (current.isLoadingMore || current.endReached) return
        val before = observedPage.value.items.size
        // The window is not even full — the local page is the whole page. Nothing more to show.
        if (before < window.value) {
            flags.value = current.copy(endReached = true)
            return
        }
        flags.value = current.copy(isLoadingMore = true)
        viewModelScope.launch {
            val result = if (observedPage.value.nextCursor != null) repo.loadMore(date) else Result.success(Unit)
            val grown = window.updateAndGet { it + TRANSPORT_PAGE_SIZE }
            val after = repo.observe(date, grown).first().items.size
            flags.value = flags.value.copy(
                isLoadingMore = false,
                isOffline = result.isFailure,
                // Only latch the end when the fetch SUCCEEDED and still produced no new row; a
                // failed network page must stay retryable, not permanently end the list.
                endReached = result.isSuccess && after <= before,
            )
        }
    }
}

@HiltViewModel class FeedTransportCaptureViewModel @Inject constructor(private val sync:SyncRepository,private val capture:ProofCaptureSource,saved:SavedStateHandle):ViewModel(){
    private val taskId=saved.get<String>(ARG_TASK_ID).orEmpty();private val shedId=saved.get<String>(ARG_SHED_ID).orEmpty();private val shedLabel=saved.get<String>(ARG_SHED_LABEL).orEmpty();private val group="feed-transport:$taskId";private val proofKey=DraftIdempotencyKey(saved,"transport_proof_key","feed-transport-video");private val submitKey=DraftIdempotencyKey(saved,"transport_submit_key","feed-transport-submit");private val proofItem=DraftOutboxItemId(saved,"transport_proof_item");private val _state=MutableStateFlow(FeedTransportCaptureUiState(shedLabel=shedLabel,videoCaptured=proofItem.value!=null));val state:StateFlow<FeedTransportCaptureUiState> = _state
    fun onEvent(e:FeedTransportCaptureEvent){when(e){FeedTransportCaptureEvent.RecordVideo->record();FeedTransportCaptureEvent.Submit->submit();FeedTransportCaptureEvent.Back->Unit}}
    private fun record(){if(_state.value.isCapturing||_state.value.videoCaptured)return;_state.update{it.copy(isCapturing=true)};viewModelScope.launch{val v=capture.captureVideo(ProofCapturePrompt.FEED_TRANSPORT);if(v==null){_state.update{it.copy(isCapturing=false)};return@launch};val req=ProofUploadRequestDto(proofType="video",mimeType=v.mimeType,scopeType="shed",scopeId=shedId,subjectType="shed",subjectId=shedId,metadata=mapOf("capture_source" to JsonPrimitive(v.captureSource),"captured_start_ms" to JsonPrimitive(v.startedAtMs),"captured_end_ms" to JsonPrimitive(v.endedAtMs)));when(val r=sync.enqueueProofUpload(group,proofKey.current(),req,v.localUri,(v.endedAtMs-v.startedAtMs).takeIf{it>0})){is AppResult.Ok->{proofItem.value=r.value;_state.update{it.copy(isCapturing=false,videoCaptured=true,message="Video queued")}};is AppResult.Err->{proofKey.invalidate();_state.update{it.copy(isCapturing=false,message=r.message)}}}}}
    private fun submit(){val proof=proofItem.value?:return;viewModelScope.launch{when(val r=sync.enqueueFeedTransportSubmit(group,submitKey.current(),taskId,proof)){is AppResult.Ok->_state.update{it.copy(status=FeedTransportSubmitStatus.QUEUED,message="Verification due")};is AppResult.Err->{submitKey.invalidate();_state.update{it.copy(status=FeedTransportSubmitStatus.FAILED,message=r.message)}}}}}
    companion object{const val ARG_TASK_ID="task_id";const val ARG_SHED_ID="shed_id";const val ARG_SHED_LABEL="shed_label"}
}
