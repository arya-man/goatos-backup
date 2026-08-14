package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery
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
    private val slots: List<FeedDistributionCapturedSlotDto>,
) : FeedRepository {
    var queries: MutableList<FeedPenSessionCaptureQuery> = mutableListOf()
        private set

    override suspend fun penSessionCaptures(
        query: FeedPenSessionCaptureQuery,
    ): List<FeedDistributionCapturedSlotDto> {
        queries += query
        return slots
    }

    override fun observeDirectionTotals(query: FeedDirectionQuery): Flow<Resource<FeedDirectionPreviewPageDto>> =
        flowOf(Resource(FeedDirectionPreviewPageDto(targetDate = "2026-08-14")))

    override fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>> =
        flowOf(PagingData.empty())

    override fun observePackingTotals(query: FeedPackingQuery): Flow<Resource<FeedPackingWorklistPageDto>> =
        flowOf(Resource(FeedPackingWorklistPageDto(targetDate = "2026-08-14")))

    override fun packingRows(query: FeedPackingQuery): Flow<PagingData<FeedPackingRowDto>> =
        flowOf(PagingData.empty())
}
