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
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.flow.updateAndGet
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.FeedTransportQuery
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportFilterUi
import sg.mesha.goatos.feature.feed.FeedTransportResultUi
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus
import sg.mesha.goatos.feature.feed.FeedTransportUiState
import sg.mesha.goatos.feature.feed.FeedDropdownOption

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
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportViewModel @Inject constructor(
    private val repo: FeedTransportRepository,
) : ViewModel() {
    private val today = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    private val query = MutableStateFlow(FeedTransportQuery(businessDate = today))
    private val flags = MutableStateFlow(FeedTransportFlags())

    // The visible window over the Room-backed SSOT. Starts at ONE page and grows on LoadMore —
    // the DAO query is re-run at the new size (ScanViewModel's `_windowSize` pattern). Before
    // this, the DAO window was a frozen `limit = 100` while `hasMore` was derived from the
    // remote cursor, so on any day with >20 rows LoadMore could fire forever against a list
    // that never changed: the rows the UI rendered could not grow, `hasMore` never went false,
    // and the list kept re-requesting — an unbounded loop (ANR risk).
    private val window = MutableStateFlow(TRANSPORT_PAGE_SIZE)

    // The window is re-observed per SCOPE (date + park + shed + status), so changing a filter
    // cannot show another filter's rows and the bound stays one page per scope.
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedPage: StateFlow<Pair<FeedTransportQuery, FeedTransportTaskPageDto>> =
        combine(query, window) { selected, size -> selected to size }
            .flatMapLatest { (selected, size) ->
                repo.observe(selected, size).map { page -> selected to page }
            }
            .stateIn(
                viewModelScope,
                SharingStarted.WhileSubscribed(5000),
                FeedTransportQuery(businessDate = today) to FeedTransportTaskPageDto(emptyList(), null),
            )

    val state: StateFlow<FeedTransportUiState> = combine(observedPage, flags, window) { (selected, page), current, size ->
        val parks = page.filters.parks.map { FeedDropdownOption(it.id, it.label) }
        val sheds = page.filters.sheds.map { FeedDropdownOption(it.id, it.label) }
        FeedTransportUiState(
            date = selected.businessDate,
            today = today,
            canCapture = selected.businessDate == today,
            filters = FeedTransportFilterUi(
                parks = parks,
                selectedParkId = selected.parkId,
                selectedParkLabel = parks.firstOrNull { it.key == selected.parkId }?.label,
                sheds = sheds,
                selectedShedId = selected.shedId,
                selectedShedLabel = sheds.firstOrNull { it.key == selected.shedId }?.label,
                status = selected.status,
            ),
            rows = page.items.map {
                // Prefer the backend-composed operational location: shedLabel alone drops the
                // partition, so a task in "Godel 1 - Part 3" would read as bare "Godel 1".
                FeedTransportRowUi(
                    it.taskId,
                    it.parkId,
                    it.shedId,
                    it.operationalLocationDisplay.ifBlank { it.shedLabel },
                    it.parkLabel,
                    it.status,
                    it.reworkReason,
                )
            },
            isRefreshing = current.isRefreshing,
            isOffline = current.isOffline,
            // Only offer more when the window is actually FULL (so growing it can yield rows) and
            // a previous LoadMore has not already proven there is nothing left. Never derived
            // from the remote cursor alone — that is what made the loop unbounded.
            hasMore = !current.endReached && page.items.size >= size,
            isLoadingMore = current.isLoadingMore,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5000),
        FeedTransportUiState(date = today, today = today),
    )

    init {
        refresh()
    }

    fun onEvent(event: FeedTransportEvent) {
        when (event) {
            FeedTransportEvent.Refresh -> refresh()
            FeedTransportEvent.LoadMore -> loadMore()
            is FeedTransportEvent.SelectDate -> selectDate(event.date)
            is FeedTransportEvent.SelectPark -> selectPark(event.parkId)
            is FeedTransportEvent.SelectShed -> updateQuery(query.value.copy(shedId = event.shedId))
            is FeedTransportEvent.SelectStatus -> updateQuery(query.value.copy(status = event.status))
            FeedTransportEvent.ClearFilters -> updateQuery(
                query.value.copy(parkId = "", shedId = "", status = ""),
            )
            is FeedTransportEvent.Open -> Unit
        }
    }

    private fun refresh() = viewModelScope.launch {
        flags.value = flags.value.copy(isRefreshing = true, isOffline = false)
        val result = repo.refresh(query.value)
        // A refresh replaces the scope's rows, so a previous "nothing left" verdict no longer holds
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
        val before = observedPage.value.second.items.size
        // The window is not even full — the local page is the whole page. Nothing more to show.
        if (before < window.value) {
            flags.value = current.copy(endReached = true)
            return
        }
        flags.value = current.copy(isLoadingMore = true)
        viewModelScope.launch {
            val scope = query.value
            val result =
                if (observedPage.value.second.nextCursor != null) repo.loadMore(scope) else Result.success(Unit)
            val grown = window.updateAndGet { it + TRANSPORT_PAGE_SIZE }
            val after = repo.observe(scope, grown).first().items.size
            flags.value = flags.value.copy(
                isLoadingMore = false,
                isOffline = result.isFailure,
                // Only latch the end when the fetch SUCCEEDED and still produced no new row; a
                // failed network page must stay retryable, not permanently end the list.
                endReached = result.isSuccess && after <= before,
            )
        }
    }

    private fun selectDate(date: LocalDate) {
        if (date.isAfter(LocalDate.parse(today))) return
        updateQuery(query.value.copy(businessDate = date.toString()))
    }

    private fun selectPark(parkId: String) {
        updateQuery(query.value.copy(parkId = parkId, shedId = ""))
    }

    private fun updateQuery(next: FeedTransportQuery) {
        if (next == query.value) return
        query.value = next
        refresh()
    }
}

/**
 * The Feed Transport per-shed capture screen.
 *
 * The recorded video and the submit key live in the shared DURABLE capture-draft store, keyed by the
 * task: they used to sit in `SavedStateHandle`, so Back + re-entry lost the clip and asked for it
 * again while the first one uploaded anyway (maintainer report 2026-07-30).
 */
@HiltViewModel class FeedTransportCaptureViewModel @Inject constructor(private val sync:SyncRepository,private val capture:ProofCaptureSource,private val drafts:CaptureDraftRepository,saved:SavedStateHandle):ViewModel(){
    private val taskId=saved.get<String>(ARG_TASK_ID).orEmpty();private val shedId=saved.get<String>(ARG_SHED_ID).orEmpty();private val shedLabel=saved.get<String>(ARG_SHED_LABEL).orEmpty();private val group="feed-transport:$taskId";private val proofKey=DraftIdempotencyKey(saved,"transport_proof_key","feed-transport-video");private var draft=CaptureDraft();private val _state=MutableStateFlow(FeedTransportCaptureUiState(shedLabel=shedLabel));val state:StateFlow<FeedTransportCaptureUiState> = _state
    init{viewModelScope.launch{draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);_state.update{it.copy(videoCaptured=draft.hasProof(STEP_VIDEO))}}}
    fun onEvent(e:FeedTransportCaptureEvent){when(e){FeedTransportCaptureEvent.RecordVideo->record();FeedTransportCaptureEvent.ReRecordVideo->reRecord();FeedTransportCaptureEvent.Submit->submit();FeedTransportCaptureEvent.Back->Unit}}
    /** Drops the discarded take's queued upload so the verifier never receives two clips, then re-captures. */
    private fun reRecord(){if(_state.value.isCapturing)return;viewModelScope.launch{draft.proofs[STEP_VIDEO]?.let{sync.deleteOutboxItem(it)};drafts.clearProof(CaptureFlow.FEED_TRANSPORT,taskId,STEP_VIDEO);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);proofKey.invalidate();_state.update{it.copy(videoCaptured=false,videoMessage=null)};record()}}
    private fun record(){if(_state.value.isCapturing||_state.value.videoCaptured)return;_state.update{it.copy(isCapturing=true)};viewModelScope.launch{val v=capture.captureVideo(ProofCapturePrompt.FEED_TRANSPORT);if(v==null){_state.update{it.copy(isCapturing=false)};return@launch};val req=ProofUploadRequestDto(proofType="video",mimeType=v.mimeType,scopeType="shed",scopeId=shedId,subjectType="shed",subjectId=shedId,metadata=mapOf("capture_source" to JsonPrimitive(v.captureSource),"captured_start_ms" to JsonPrimitive(v.startedAtMs),"captured_end_ms" to JsonPrimitive(v.endedAtMs)));when(val r=sync.enqueueProofUpload(group,proofKey.current(),req,v.localUri,(v.endedAtMs-v.startedAtMs).takeIf{it>0})){is AppResult.Ok->{drafts.putProof(CaptureFlow.FEED_TRANSPORT,taskId,STEP_VIDEO,r.value);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);_state.update{it.copy(isCapturing=false,videoCaptured=true,videoMessage="Video queued")}};is AppResult.Err->{proofKey.invalidate();_state.update{it.copy(isCapturing=false,videoMessage=r.message)}}}}}
    private fun submit(){val proof=draft.proofs[STEP_VIDEO]?:return;viewModelScope.launch{
        // STABLE per task and durable, so a re-entered screen resends the SAME key.
        val submitIdempotencyKey=draft.submitIdempotencyKey?:"feed-transport-submit:$taskId"
        if(draft.submitIdempotencyKey==null){drafts.putSubmit(CaptureFlow.FEED_TRANSPORT,taskId,submitIdempotencyKey,null);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId)}
        when(val r=sync.enqueueFeedTransportSubmit(group,submitIdempotencyKey,taskId,proof)){is AppResult.Ok->{drafts.putSubmit(CaptureFlow.FEED_TRANSPORT,taskId,submitIdempotencyKey,r.value);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);_state.update{it.copy(result=FeedTransportResultUi(FeedTransportSubmitStatus.QUEUED,"Submitted for verification"))}};is AppResult.Err->_state.update{it.copy(result=FeedTransportResultUi(FeedTransportSubmitStatus.FAILED,r.message))}}}}
    companion object{const val ARG_TASK_ID="task_id";const val ARG_SHED_ID="shed_id";const val ARG_SHED_LABEL="shed_label";private const val STEP_VIDEO="video"}
}
