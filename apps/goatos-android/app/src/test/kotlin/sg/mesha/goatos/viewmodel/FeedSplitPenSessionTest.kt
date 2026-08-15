package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery
import sg.mesha.goatos.core.data.FeedPenSessionCaptures
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturedSlotDto
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto
import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.FeedDirectionQuery
import sg.mesha.goatos.core.data.FeedPackingQuery

/**
 * A feed repository that reports proofs recorded on OTHER operators' phones.
 *
 * The split pen-session is the whole point: one operator shoots the weight photo, another the feed
 * video, another the water video. Before the 2026-08-14 shared read, a proof was visible only on the
 * device that shot it, so no phone held all three references and the pen could not be submitted at
 * all.
 */
class FakeSplitFeedRepository(
    var slots: List<FeedDistributionCapturedSlotDto>,
    var sessionStatus: String? = null,
) : FeedRepository {
    var queries: MutableList<FeedPenSessionCaptureQuery> = mutableListOf()
        private set

    /** Number of leading penSessionCaptures calls that fail (return null) before [slots] is served. */
    var failuresBeforeSuccess: Int = 0

    override suspend fun persistDirectionSessionStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, lifecycleStatus: String) = Unit
    override suspend fun persistPackingRowStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, lifecycleStatus: String) = Unit
    override suspend fun penSessionCaptures(
        query: FeedPenSessionCaptureQuery,
    ): FeedPenSessionCaptures? {
        queries += query
        if (failuresBeforeSuccess > 0) {
            failuresBeforeSuccess--
            return null
        }
        return FeedPenSessionCaptures(slots = slots, sessionStatus = sessionStatus)
    }

    override suspend fun probeDirectionSummary(query: FeedDirectionQuery): Boolean = true
    override suspend fun fetchProofDownloadUrl(proofId: String): String? =
        if (proofId.isNotBlank()) "https://stg.example.com/proofs/$proofId/download?token=xyz" else null

    override fun observeDirectionTotals(query: FeedDirectionQuery): Flow<Resource<FeedDirectionPreviewPageDto>> =
        flowOf(Resource(FeedDirectionPreviewPageDto(targetDate = "2026-08-14")))

    override fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>> =
        flowOf(PagingData.empty())

    override fun observePackingTotals(query: FeedPackingQuery): Flow<Resource<FeedPackingWorklistPageDto>> =
        flowOf(Resource(FeedPackingWorklistPageDto(targetDate = "2026-08-14")))

    override fun packingRows(query: FeedPackingQuery): Flow<PagingData<FeedPackingRowDto>> =
        flowOf(PagingData.empty())

    override fun observePackingRowStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int): Flow<String?> = flowOf(null)
    override fun observeDirectionSessionStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int): Flow<String?> = flowOf(null)
    override suspend fun fetchDirectionSessionStatus(parkId: String, shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, targetDate: String): String? = null
    override suspend fun fetchPackingRowStatus(parkId: String, shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, targetDate: String): String? = null
}
