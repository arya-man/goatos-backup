package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.flow.updateAndGet
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.ui.operationalLocationLabel
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.FeedTransportStatusSource
import sg.mesha.goatos.core.data.FeedTransportQuery
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportFilterUi
import sg.mesha.goatos.feature.feed.FeedTransportResultUi
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus
import sg.mesha.goatos.feature.feed.FeedTransportUiState
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

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
    private val feedCompletionStore: FeedCompletionLocalStore,
    private val repo: FeedTransportRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
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

    val state: StateFlow<FeedTransportUiState> = combine(
        observedPage,
        flags,
        window,
        feedCompletionStore.submittedForReviewKeys,
    ) { (selected, page), current, size, locallySubmitted ->
        val parks = page.filters.parks.map { FeedDropdownOption(it.id, it.label) }
        val sheds = page.filters.sheds.map {
            FeedDropdownOption(it.id, it.label)
        }
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
                // The option key IS the shed id: one shed is one transport task, so there is no
                // pen key to reassemble.
                selectedShedLabel = sheds.firstOrNull { it.key == selected.shedId }?.label,
                status = selected.status,
            ),
            rows = page.items.map {
                // Render the backend-composed location verbatim. A transport task is shed-grain, so
                // this is the bare shed name; the fallback still composes a partition because a
                // task recorded while the grain was briefly per-pen keeps naming its pen.
                FeedTransportRowUi(
                    it.taskId,
                    it.parkId,
                    it.shedId,
                    it.operationalLocationDisplay.ifBlank { operationalLocationLabel(it.shedLabel, it.partitionLabel) },
                    it.parkLabel,
                    // A submit that is still only in the outbox leaves the backend status at "due",
                    // so the chip read "Pending" for work the operator had already sent (254.mp4,
                    // same defect fixed for Packing/Direction/Distribution). Transport is
                    // TASK-grain, hence transportKey rather than the shed-session key.
                    overlayTransportStatus(
                        it.status,
                        it.reworkReason,
                        locallySubmitted.contains(FeedCompletionLocalStore.taskKey("feed-transport", it.taskId)),
                    ),
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
        analytics.track(AnalyticsEvents.FEED_TRANSPORT_VIEWED)
        refresh()
    }

    fun onEvent(event: FeedTransportEvent) {
        when (event) {
            FeedTransportEvent.Refresh -> refresh()
            FeedTransportEvent.LoadMore -> loadMore()
            is FeedTransportEvent.SelectDate -> selectDate(event.date)
            is FeedTransportEvent.SelectPark -> selectPark(event.parkId)
            is FeedTransportEvent.SelectShed -> if (updateQuery(query.value.copy(shedId = event.shedId))) {
                trackFilter(DIMENSION_SHED, event.shedId)
            }
            is FeedTransportEvent.SelectStatus -> if (updateQuery(query.value.copy(status = event.status))) {
                trackFilter(DIMENSION_STATUS, event.status)
            }
            FeedTransportEvent.ClearFilters -> if (updateQuery(query.value.copy(parkId = "", shedId = "", status = ""))) {
                trackFilter(DIMENSION_ALL, "")
            }
            is FeedTransportEvent.Open -> analytics.track(
                AnalyticsEvents.FEED_ROW_TAPPED,
                mapOf(
                    AnalyticsEvents.Params.KIND to KIND_TRANSPORT,
                    AnalyticsEvents.Params.SHED_ID to event.row.shedId,
                ),
            )
        }
    }

    private fun refresh() = viewModelScope.launch {
        flags.value = flags.value.copy(isRefreshing = true, isOffline = false)
        val result = repo.refresh(query.value)
        // A refresh replaces the scope's rows, so a previous "nothing left" verdict no longer holds
        // and the window goes back to one page — the same reset a fresh screen entry would give.
        window.value = TRANSPORT_PAGE_SIZE
        flags.value = flags.value.copy(isRefreshing = false, isOffline = result.exceptionOrNull().isConnectivityFailure(), endReached = false)
        result.exceptionOrNull()?.let { error ->
            crashReporter.recordException(error, "feed transport refresh failed")
            analytics.track(
                AnalyticsEvents.FEED_READ_FAILURE,
                mapOf(
                    AnalyticsEvents.Params.KIND to KIND_TRANSPORT,
                    AnalyticsEvents.Params.REASON to analyticsReason(error),
                ),
            )
        }
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
                isOffline = result.exceptionOrNull().isConnectivityFailure(),
                // Only latch the end when the fetch SUCCEEDED and still produced no new row; a
                // failed network page must stay retryable, not permanently end the list.
                endReached = result.isSuccess && after <= before,
            )
            result.exceptionOrNull()?.let { error ->
                crashReporter.recordException(error, "feed transport next page failed")
                analytics.track(
                    AnalyticsEvents.FEED_READ_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.KIND to KIND_TRANSPORT,
                        AnalyticsEvents.Params.REASON to analyticsReason(error),
                    ),
                )
            }
        }
    }

    private fun selectDate(date: LocalDate) {
        if (date.isAfter(LocalDate.parse(today))) return
        if (updateQuery(query.value.copy(businessDate = date.toString()))) {
            trackFilter(DIMENSION_DATE, date.toString())
        }
    }

    private fun selectPark(parkId: String) {
        if (updateQuery(query.value.copy(parkId = parkId, shedId = ""))) {
            trackFilter(DIMENSION_FARM, parkId)
        }
    }

    private fun updateQuery(next: FeedTransportQuery): Boolean {
        if (next == query.value) return false
        query.value = next
        refresh()
        return true
    }

    private fun trackFilter(dimension: String, value: String) {
        analytics.track(
            AnalyticsEvents.FEED_FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to dimension,
                AnalyticsEvents.Params.ACTION to if (value.isBlank()) ACTION_CLEARED else ACTION_SET,
            ),
        )
    }

    private companion object {
        const val KIND_TRANSPORT = "transport"
        const val DIMENSION_FARM = "farm"
        const val DIMENSION_SHED = "shed"
        const val DIMENSION_STATUS = "status"
        const val DIMENSION_DATE = "date"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}

private fun analyticsReason(error: Throwable): String =
    error::class.java.simpleName.ifBlank { "unknown" }

/**
 * The Feed Transport per-shed capture screen.
 *
 * The recorded video and the submit key live in the shared DURABLE capture-draft store, keyed by the
 * task: they used to sit in `SavedStateHandle`, so Back + re-entry lost the clip and asked for it
 * again while the first one uploaded anyway (maintainer report 2026-07-30).
 */
@HiltViewModel class FeedTransportCaptureViewModel @Inject constructor(private val sync:SyncRepository,private val feedCompletionStore:FeedCompletionLocalStore,private val capture:ProofCaptureSource,private val proofCaptureRepository:ProofCaptureRepository,private val drafts:CaptureDraftRepository,private val analytics:AnalyticsPort,private val crashReporter:CrashReporter,private val feedTransportRepository:FeedTransportStatusSource,saved:SavedStateHandle):ViewModel(){
    private val taskId=saved.get<String>(ARG_TASK_ID).orEmpty();private val shedId=saved.get<String>(ARG_SHED_ID).orEmpty();private val shedLabel=saved.get<String>(ARG_SHED_LABEL).orEmpty();private val parkLabel=saved.get<String>(ARG_PARK_LABEL).orEmpty();private val group="feed-transport:$taskId";private val proofKey=DraftIdempotencyKey(saved,"transport_proof_key","feed-transport-video");private var draft=CaptureDraft();private var proofRowId:String?=null
    // The server poll (fetchTaskStatus) needs a business date and transport tasks are always
    // TODAY's only (see alreadySubmitted's isToday=true below) -- mirrors FeedTransportViewModel's
    // own `today`.
    private val today=LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    // The task's backend-owned status AT THE MOMENT the row was tapped — a FIRST-PAINT hint only.
    // [observeLiveLifecycleStatus] supersedes it with the Room-backed live value the moment Room has
    // one, so a status change while this screen stays open flips it to read-only live rather than on
    // next entry. Mirrors FeedPackingCompleteViewModel.observeLiveLifecycleStatus (STG 2026-08-09).
    private val lifecycleStatusHint:String=saved.get<String>(ARG_LIFECYCLE_STATUS).orEmpty()
    private val alreadySubmitted:Boolean=!feedSessionCanCapture(lifecycleStatusHint,isToday=true)
    private val _state=MutableStateFlow(FeedTransportCaptureUiState(shedLabel=shedLabel,alreadySubmitted=alreadySubmitted));val state:StateFlow<FeedTransportCaptureUiState> = _state
    init{analytics.track(AnalyticsEvents.FEED_TRANSPORT_OPENED,mapOf(AnalyticsEvents.Params.SHED_ID to shedId));viewModelScope.launch{draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);_state.update{it.copy(videoCaptured=draft.hasProof(STEP_VIDEO))};draft.proofs[STEP_VIDEO]?.let(::observeProofItem);draft.submitOutboxItemId?.let(::observeOutboxItem)};observeSyncStatus();observeDurableProof();observeLiveLifecycleStatus()}
    /** A `null` emission (no cached row for this task yet) is ignored so the screen keeps
     *  [lifecycleStatusHint] rather than forcing itself editable. */
    private fun observeLiveLifecycleStatus(){viewModelScope.launch{feedTransportRepository.observeTaskStatus(taskId).collect{liveStatus->applyLiveStatus(liveStatus)}};startServerStatusPolling()}

    /** Shared by the Room-backed observer above and the SERVER poll below. `null` (no answer yet /
     *  poll failed) is ignored: this must never flip editable -> locked on a guess, and never flips
     *  locked -> editable at all. */
    private fun applyLiveStatus(liveStatus:String?){if(liveStatus==null)return;_state.update{it.copy(alreadySubmitted=!feedSessionCanCapture(liveStatus,isToday=true))}}

    /**
     * Periodic SERVER read of this transport task's status while the screen stays open, so a
     * TEAMMATE'S submit on another phone flips this screen read-only without back/reopen --
     * [observeTaskStatus] above only changes when THIS phone's own list refresh writes a fresh Room
     * row. Reuses the existing `GET /feed-transport/tasks` read via
     * [FeedTransportStatusSource.fetchTaskStatus] (no new backend endpoint). See
     * [FeedDistributionCompleteViewModel.startServerStatusPolling]'s kdoc for why this is bounded at
     * [MAX_SERVER_STATUS_POLLS] rather than a bare `while (isActive)`.
     */
    private fun startServerStatusPolling(){viewModelScope.launch{repeat(MAX_SERVER_STATUS_POLLS){delay(SERVER_STATUS_POLL_INTERVAL_MS);pollServerStatusOnce()}}}

    /** A poll failure (offline/timeout/5xx) is swallowed and leaves state exactly as it was -- see
     *  [applyLiveStatus]'s null-is-unknown contract. */
    // exception:exempt expected poll failure (offline/timeout/5xx); see kdoc above — null-is-unknown is the contract, not an error to record
    private suspend fun pollServerStatusOnce(){if(shedId.isBlank()||taskId.isBlank())return;val status=runCatching{feedTransportRepository.fetchTaskStatus(today,shedId,taskId)}.getOrNull();applyLiveStatus(status)}

    /**
     * Follow the queued submit to its REAL outcome.
     *
     * Enqueuing only means the write reached the phone's outbox. Transport reported that as
     * "Submitted for verification" and then never looked again, so when the server refused the
     * submit the screen kept claiming success: the operator saw no status change and was still
     * offered Submit and Re-record, with nothing anywhere telling him it had failed (reported
     * 2026-08-09, against a 403 the backend was returning for a park-scoped operator). Packing and
     * distribution have always observed their item; transport was the one that did not.
     */
    private fun observeOutboxItem(itemId:String){
        statusJob?.cancel()
        statusJob=viewModelScope.launch{
            sync.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect{item->
                    val write=item.toWriteResult("Submitted for verification","Submitted for verification")
                    if(!write.isCommitted&&_state.value.result?.status!=FeedTransportSubmitStatus.FAILED){crashReporter.log("feed transport submit failed item=$itemId");analytics.track(AnalyticsEvents.FEED_TRANSPORT_FAILURE,mapOf(AnalyticsEvents.Params.REASON to item.writeFailureReason()))}
                    // Reset submitInFlight latch on terminal failure so the user can retry.
                    if(item.status==SyncItemStatus.FAILED){
                        submitInFlight=false
                    }
                    _state.update{it.copy(result=FeedTransportResultUi(write.status.toTransportStatus(),write.message.orEmpty()))}
                }
        }
    }
    fun onEvent(e:FeedTransportCaptureEvent){when(e){FeedTransportCaptureEvent.RecordVideo->record();FeedTransportCaptureEvent.ReRecordVideo->reRecord();FeedTransportCaptureEvent.Submit->submit();FeedTransportCaptureEvent.SyncNow->syncNow();FeedTransportCaptureEvent.Back->Unit}}
    /** Manohar ordering: the new video is captured AND durably persisted via
     *  [ProofCaptureRepository.captureReplacingLatest] before the OLD row is ever touched -- that
     *  repository call only removes the previous active row(s) for the slot AFTER the new capture
     *  returns [AppResult.Ok], so a camera cancel, a capture-repo failure, or process death mid-flow
     *  all leave the old proof exactly where it was. Same contract as
     *  [FeedDistributionCompleteViewModel]'s capture functions. */
    private fun reRecord(){if(_state.value.isCapturing)return;record(replacing=true)}
    private fun record(replacing:Boolean=false){if(_state.value.isCapturing||_state.value.alreadySubmitted||(_state.value.videoCaptured&&!replacing))return;_state.update{it.copy(isCapturing=true)};viewModelScope.launch{var captureThrew=false;val v=try{capture.captureVideo(ProofCaptureContext(title=feedTransportProofCaption(),primaryTag=shedLabel.ifBlank{shedId},workLabel="Transport",prompt=ProofCapturePrompt.FEED_TRANSPORT))}catch(error:Exception){crashReporter.recordException(error,"feed transport video capture failed");captureThrew=true;null};if(v==null){_state.update{it.copy(isCapturing=false,videoMessage=if(captureThrew)"Video capture failed. Try again." else it.videoMessage)};return@launch};val slot=EvidenceSlot(identity=ProofIdentity(flow=ProofFlow.FEED_TRANSPORT,taskId=group,shedId=shedId,subjectKey=shedId),fieldKey=FIELD_FEED_TRANSPORT_VIDEO);when(val r=proofCaptureRepository.captureReplacingLatest(slot=slot,subject=ProofSubject.SHED,subjectId=shedId,localUri=v.localUri,mimeType=v.mimeType,caption=feedTransportProofCaption(),scopeType="shed",scopeId=shedId,capturedStartMs=v.startedAtMs,capturedEndMs=v.endedAtMs,capturedByPrincipalId=null,proofPolicy=feedShedProofPolicy(v.captureSource),awaitUploadEnqueue=true,uploadGroupKey=group)){is AppResult.Ok->{proofRowId=r.value.id;val proofOutboxId=r.value.outboxItemId;if(proofOutboxId.isNullOrBlank()){_state.update{it.copy(isCapturing=false,videoCaptured=false,videoMessage="Video could not be queued")};return@launch};if(replacing){drafts.clearProof(CaptureFlow.FEED_TRANSPORT,taskId,STEP_VIDEO);proofKey.invalidate()};drafts.putProof(CaptureFlow.FEED_TRANSPORT,taskId,STEP_VIDEO,proofOutboxId);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);observeProofItem(proofOutboxId);analytics.track(AnalyticsEvents.FEED_TRANSPORT_VIDEO_CAPTURED);_state.update{it.copy(isCapturing=false,videoCaptured=true,videoMessage="Video queued")}};is AppResult.Err->{proofKey.invalidate();r.cause?.let{crashReporter.recordException(it,"feed transport video enqueue failed")};analytics.track(AnalyticsEvents.FEED_TRANSPORT_FAILURE,mapOf(AnalyticsEvents.Params.REASON to r.analyticsReason("video_enqueue_failed")));_state.update{it.copy(isCapturing=false,videoCaptured=false,videoMessage=r.message)}}}}}

    private fun feedTransportProofCaption():String=proofOverlayContextLine("Feed transport",parkLabel,shedLabel.ifBlank{shedId})
    private var proofStatusJob:Job?=null
    private fun observeProofItem(itemId:String){proofStatusJob?.cancel();proofStatusJob=viewModelScope.launch{sync.observeItem(itemId).filterNotNull().distinctUntilChanged().collect(::updateProofStatus)}}
    private fun observeDurableProof(){viewModelScope.launch{proofCaptureRepository.observeProofs(group).collect{rows->val row=rows.filter{it.fieldKey==FIELD_FEED_TRANSPORT_VIDEO&&it.syncStatus!=CaptureSyncStatus.FAILED}.maxByOrNull{it.capturedAtMs}?:return@collect;hydrateFromProof(row)}}}
    private suspend fun hydrateFromProof(row:ProofCaptureRow){proofRowId=row.id;row.outboxItemId?.takeIf{it.isNotBlank()}?.let{outboxId->if(draft.proofs[STEP_VIDEO]!=outboxId){drafts.putProof(CaptureFlow.FEED_TRANSPORT,taskId,STEP_VIDEO,outboxId);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId)};observeProofItem(outboxId)};val status=row.toProofStatus();_state.update{it.copy(videoCaptured=true,videoPreviewPath=row.previewUri()?:it.videoPreviewPath,videoStatus=status,videoMessage=row.toProofMessage(status,"Transport video saved on this phone. It will upload automatically.","Transport video upload is in progress.","Transport video is ready.","Video could not be queued"),canSubmit=status.isQueuedForSubmit())}}
    private fun updateProofStatus(item:SyncQueueItem){val proofStatus=when(item.status){SyncItemStatus.QUEUED->FeedDistributionProofStatus.QUEUED;SyncItemStatus.IN_FLIGHT->FeedDistributionProofStatus.UPLOADING;SyncItemStatus.SUCCEEDED->FeedDistributionProofStatus.SYNCED;SyncItemStatus.FAILED->FeedDistributionProofStatus.FAILED};val message=when(proofStatus){FeedDistributionProofStatus.QUEUED->"Transport video saved on this phone. It will upload automatically.";FeedDistributionProofStatus.UPLOADING->"Transport video upload is in progress.";FeedDistributionProofStatus.SYNCED->"Transport video is ready.";FeedDistributionProofStatus.FAILED->item.lastError?:"Video could not be queued";FeedDistributionProofStatus.EMPTY->null};_state.update{it.copy(videoCaptured=it.videoCaptured||item.localFilePath!=null,videoPreviewPath=item.localFilePath?:it.videoPreviewPath,videoStatus=proofStatus,videoMessage=message,canSubmit=proofStatus.isQueuedForSubmit())}}
    private var statusJob:Job?=null
    private var syncStatusJob:Job?=null
    private fun observeSyncStatus(){syncStatusJob?.cancel();syncStatusJob=viewModelScope.launch{sync.observeStatus().map{it.inFlightCount>0}.distinctUntilChanged().collect{syncing->_state.update{it.copy(isSyncing=syncing)}}}}
    private fun syncNow(){viewModelScope.launch{sync.triggerDrain();pollServerStatusOnce()}}
    // Established idiom (SubmitViewModel.submitInFlight): a plain latch checked-and-set BEFORE the
    // enqueue coroutine launches, so a second tap landing in the async gap between the tap and the
    // state update reflecting it (`result`/`canSubmit`) cannot slip past submitEnabled and enqueue
    // a second write. Outbox idempotency-key dedup alone was not enough — it collapses a RETRY of
    // the identical payload, but two concurrent taps that both read submitEnabled=true before either
    // write lands would still both reach sync.enqueueFeedTransportSubmit. Reset on any terminal
    // outcome (success or enqueue failure) so a real failure stays retryable.
    private var submitInFlight = false
    private fun submit(){val current=_state.value;val proof=draft.proofs[STEP_VIDEO];if(submitInFlight||proof.isNullOrBlank()||!current.submitEnabled){_state.update{it.copy(canSubmit=false,videoMessage="Record the transport video before submitting.")};return};submitInFlight=true;viewModelScope.launch{
        val submitIdempotencyKey="feed-transport-submit:$taskId:$proof"
        if(draft.submitIdempotencyKey!=submitIdempotencyKey){drafts.putSubmit(CaptureFlow.FEED_TRANSPORT,taskId,submitIdempotencyKey,null);draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId)}
        when(val r=sync.enqueueFeedTransportSubmit(group,submitIdempotencyKey,taskId,proof)){is AppResult.Ok->{drafts.putSubmit(CaptureFlow.FEED_TRANSPORT,taskId,submitIdempotencyKey,r.value);feedCompletionStore.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", taskId));draft=drafts.find(CaptureFlow.FEED_TRANSPORT,taskId);observeOutboxItem(r.value);analytics.track(AnalyticsEvents.FEED_TRANSPORT_SUBMITTED);_state.update{it.copy(result=FeedTransportResultUi(FeedTransportSubmitStatus.QUEUED,"Submitted for verification"))}};is AppResult.Err->{submitInFlight=false;r.cause?.let{crashReporter.recordException(it,"feed transport complete enqueue failed")};analytics.track(AnalyticsEvents.FEED_TRANSPORT_FAILURE,mapOf(AnalyticsEvents.Params.REASON to r.analyticsReason("submit_enqueue_failed")));_state.update{it.copy(result=FeedTransportResultUi(FeedTransportSubmitStatus.FAILED,r.message))}}}}}
    companion object{const val ARG_TASK_ID="task_id";const val ARG_SHED_ID="shed_id";const val ARG_SHED_LABEL="shed_label";const val ARG_PARK_LABEL="park_label";const val ARG_LIFECYCLE_STATUS="lifecycle_status";private const val STEP_VIDEO="video";private const val FIELD_FEED_TRANSPORT_VIDEO="feed_transport_video"
        /** How often [startServerStatusPolling] re-checks this task's status directly from the
         *  server while the screen stays open. */
        private const val SERVER_STATUS_POLL_INTERVAL_MS = 30_000L
        /** Bound for [startServerStatusPolling] — see its kdoc for why this cannot be unbounded. */
        private const val MAX_SERVER_STATUS_POLLS = 2_880
    }
}

private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toTransportStatus(): FeedTransportSubmitStatus = when (this) {
    sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedTransportSubmitStatus.SYNCED
    sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedTransportSubmitStatus.FAILED
    else -> FeedTransportSubmitStatus.QUEUED
}

private fun SyncQueueItem.writeFailureReason(): String = when {
    conflict -> "conflict"
    isDeadLetter -> "attempts_exhausted"
    else -> "failed"
}

private fun AppResult.Err.analyticsReason(fallback: String): String =
    cause?.let(::analyticsReason) ?: fallback

/**
 * The status a Feed TRANSPORT row RENDERS. Transport uses its own vocabulary ("due" /
 * "verification_due" / "completed"), so it cannot reuse [overlayFeedLifecycleStatus] verbatim, but
 * the precedence is identical: server acceptance wins, then a rework reason (never mask a
 * rejection), then this phone's queued submit, else the backend status.
 */
internal fun overlayTransportStatus(
    status: String,
    reworkReason: String?,
    isLocallySubmittedForReview: Boolean,
): String = when {
    status == "completed" -> "completed"
    !reworkReason.isNullOrBlank() -> status
    isLocallySubmittedForReview -> "verification_due"
    else -> status
}
