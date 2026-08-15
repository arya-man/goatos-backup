package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery
import sg.mesha.goatos.core.data.FeedPenSessionCaptures
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturedSlotDto
import androidx.paging.PagingData
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.FeedDirectionQuery
import sg.mesha.goatos.core.data.FeedPackingQuery
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.FeedTransportStatusSource
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto

/**
 * Minimal [FeedRepository] fake for tests that only exercise the live per-row lifecycle-status
 * observers ([observePackingRowStatus] / [observeDirectionSessionStatus]). Every other member is
 * unused by the packing/distribution completion ViewModels and errors if ever called, matching the
 * `error("unused")` idiom used by this test package's other fakes.
 *
 * A single [MutableStateFlow] models "the same Room row the worklist renders from" — tests drive it
 * with [emitPackingStatus] / [emitDirectionStatus] to model a status flip landing WHILE the
 * completion screen is open (a verifier decision arriving, or another device's write draining).
 */
internal class FakeFeedRepository : FeedRepository {
    var penSessionStatus: String? = null
    override suspend fun penSessionCaptures(query: FeedPenSessionCaptureQuery): FeedPenSessionCaptures? = FeedPenSessionCaptures(emptyList(), penSessionStatus)

    private val packingStatus = MutableStateFlow<String?>(null)
    private val directionStatus = MutableStateFlow<String?>(null)
    private var delayedPackingStatus: String? = null
    private var delayedPackingDelayMs: Long = 0
    private var delayedDirectionStatus: String? = null
    private var delayedDirectionDelayMs: Long = 0

    // --- server poll (fetchDirectionSessionStatus / fetchPackingRowStatus) ---
    // Queues of answers consumed one-per-call so a test can script "poll 1 returns X, poll 2
    // returns Y (or throws)". An empty queue falls back to [defaultServerDirectionStatus] /
    // [defaultServerPackingStatus] (null by default, i.e. "server poll found nothing / not called").
    private val directionServerStatusQueue = ArrayDeque<Result<String?>>()
    private val packingServerStatusQueue = ArrayDeque<Result<String?>>()
    var defaultServerDirectionStatus: String? = null
    var defaultServerPackingStatus: String? = null
    var directionServerFetchCalls: Int = 0
        private set
    var packingServerFetchCalls: Int = 0
        private set

    /** Queues the next [fetchDirectionSessionStatus] answer. */
    fun queueServerDirectionStatus(status: String?) {
        directionServerStatusQueue.addLast(Result.success(status))
    }

    /** Queues the next [fetchDirectionSessionStatus] call to FAIL (simulating offline/timeout/5xx). */
    fun queueServerDirectionFailure() {
        directionServerStatusQueue.addLast(Result.failure(IllegalStateException("simulated poll failure")))
    }

    /** Queues the next [fetchPackingRowStatus] answer. */
    fun queueServerPackingStatus(status: String?) {
        packingServerStatusQueue.addLast(Result.success(status))
    }

    /** Queues the next [fetchPackingRowStatus] call to FAIL (simulating offline/timeout/5xx). */
    fun queueServerPackingFailure() {
        packingServerStatusQueue.addLast(Result.failure(IllegalStateException("simulated poll failure")))
    }

    fun emitPackingStatus(status: String?) {
        packingStatus.value = status
    }

    fun emitPackingStatusWithDelay(status: String?, delayMs: Long) {
        delayedPackingStatus = status
        delayedPackingDelayMs = delayMs
    }

    fun emitDirectionStatus(status: String?) {
        directionStatus.value = status
    }

    fun emitDirectionStatusWithDelay(status: String?, delayMs: Long) {
        delayedDirectionStatus = status
        delayedDirectionDelayMs = delayMs
    }

    override fun observePackingRowStatus(
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: Int,
    ): Flow<String?> = if (delayedPackingStatus != null) {
        flow {
            delay(delayedPackingDelayMs)
            emit(delayedPackingStatus)
        }
    } else {
        packingStatus
    }

    override fun observeDirectionSessionStatus(
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: Int,
    ): Flow<String?> = if (delayedDirectionStatus != null) {
        flow {
            delay(delayedDirectionDelayMs)
            emit(delayedDirectionStatus)
        }
    } else {
        directionStatus
    }

    override suspend fun fetchDirectionSessionStatus(
        parkId: String,
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: Int,
        targetDate: String,
    ): String? {
        directionServerFetchCalls += 1
        if (directionServerStatusQueue.isNotEmpty()) return directionServerStatusQueue.removeFirst().getOrThrow()
        return defaultServerDirectionStatus
    }

    override suspend fun fetchPackingRowStatus(
        parkId: String,
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: Int,
        targetDate: String,
    ): String? {
        packingServerFetchCalls += 1
        if (packingServerStatusQueue.isNotEmpty()) return packingServerStatusQueue.removeFirst().getOrThrow()
        return defaultServerPackingStatus
    }

    override suspend fun probeDirectionSummary(query: FeedDirectionQuery): Boolean = true
    override suspend fun fetchProofDownloadUrl(proofId: String): String? = null
    override fun observeDirectionTotals(query: FeedDirectionQuery): Flow<Resource<FeedDirectionPreviewPageDto>> = error("unused")
    override fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>> = error("unused")
    override fun observePackingTotals(query: FeedPackingQuery): Flow<Resource<FeedPackingWorklistPageDto>> = error("unused")
    override fun packingRows(query: FeedPackingQuery): Flow<PagingData<FeedPackingRowDto>> = error("unused")
}

/**
 * Test-driven [FeedTransportStatusSource] — the whole reason [FeedTransportRepository] was split
 * into a narrow interface, so this needs no real [sg.mesha.goatos.core.data.GoatDatabase].
 */
internal class FakeFeedTransportStatusSource : FeedTransportStatusSource {
    private val status = MutableStateFlow<String?>(null)
    private var delayedStatus: String? = null
    private var delayedDelayMs: Long = 0

    fun emit(status: String?) {
        this.status.value = status
    }

    fun emitWithDelay(status: String?, delayMs: Long) {
        delayedStatus = status
        delayedDelayMs = delayMs
    }

    override fun observeTaskStatus(taskId: String): Flow<String?> = if (delayedStatus != null) {
        flow {
            delay(delayedDelayMs)
            emit(delayedStatus)
        }
    } else {
        status
    }

    // --- server poll (fetchTaskStatus) --- same queue idiom as FakeFeedRepository above.
    private val serverStatusQueue = ArrayDeque<Result<String?>>()
    var defaultServerStatus: String? = null
    var serverFetchCalls: Int = 0
        private set

    fun queueServerStatus(status: String?) {
        serverStatusQueue.addLast(Result.success(status))
    }

    fun queueServerFailure() {
        serverStatusQueue.addLast(Result.failure(IllegalStateException("simulated poll failure")))
    }

    override suspend fun fetchTaskStatus(businessDate: String, shedId: String, taskId: String): String? {
        serverFetchCalls += 1
        if (serverStatusQueue.isNotEmpty()) return serverStatusQueue.removeFirst().getOrThrow()
        return defaultServerStatus
    }
}
