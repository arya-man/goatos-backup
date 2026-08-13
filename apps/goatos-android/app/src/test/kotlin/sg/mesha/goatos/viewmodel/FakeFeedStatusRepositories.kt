package sg.mesha.goatos.viewmodel

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
    private val packingStatus = MutableStateFlow<String?>(null)
    private val directionStatus = MutableStateFlow<String?>(null)
    private var delayedPackingStatus: String? = null
    private var delayedPackingDelayMs: Long = 0
    private var delayedDirectionStatus: String? = null
    private var delayedDirectionDelayMs: Long = 0

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
}
