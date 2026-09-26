package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SalesDealScopeMeta
import sg.mesha.goatos.core.data.SalesLeadSide
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidatePageDto
import sg.mesha.goatos.core.network.dto.SaleLocationEntryDto
import sg.mesha.goatos.core.network.dto.SaleLocationParkDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SalePreviewDto
import sg.mesha.goatos.core.network.dto.SaleTaggingDealDto
import sg.mesha.goatos.core.network.dto.SaleTaggingQueueDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadDto
import sg.mesha.goatos.core.network.dto.SalesLeadBoardMetaDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.feature.vendors.SaleTaggingEvent
import sg.mesha.goatos.rfid.FakeScanSource
import sg.mesha.goatos.ui.Routes

/**
 * THE TAGGING SCREEN SEARCHES THE SALE'S OWN PARK (review of PR #446).
 *
 * The screen used to work out the park by matching the sale's farm code against the pen catalog,
 * read in parallel with the queue cache. A tenant-wide park head -- the real ones today -- sees two
 * parks, so when the catalog landed before the queue row nothing matched, the park stayed blank
 * for good and every lookup answered "Could not work out your park". A sale that was not on the
 * cached first page of the queue never had a row at all. The park now comes from the server's
 * park_id on the sale's own read, so neither the order of arrival nor the queue page matters.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class SaleTaggingViewModelParkTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a tenant-wide park head searches the park the server named for the sale, whatever arrives first`() = runTest(dispatcher) {
        val repo = ParkRaceSalesRepository()
        val vm = SaleTaggingViewModel(
            savedStateHandle = SavedStateHandle(mapOf(Routes.SALE_ID_ARG to "deal-cbe")),
            repository = repo,
            scanSource = FakeScanSource(),
            analytics = SilentAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(SaleTaggingEvent.TagInputChanged("9051"))
        vm.onEvent(SaleTaggingEvent.Lookup)
        advanceUntilIdle()

        assertEquals("the lookup must search the CBE park the server resolved", listOf("park-cbe"), repo.searchedParks)
        assertTrue(
            "the screen must never say it could not work out the park",
            !vm.state.value.lookupMessage.orEmpty().contains("Could not work out your park"),
        )
        job.cancel()
    }
}

private class SilentAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

/**
 * The race, reproduced: the queue's cached first page does NOT hold this sale, and the pen catalog
 * names TWO parks (a tenant-wide caller). Only the sale's own read carries its park.
 */
private class ParkRaceSalesRepository : SalesRepository {
    val searchedParks = mutableListOf<String>()
    private val deal = SaleTaggingDealDto(
        salesDealId = "deal-cbe", saleDate = "2026-09-26", farm = "CBE", parkId = "park-cbe",
        productType = "Goat", breed = "Boer", declaredAnimalCount = 2, alreadyTagged = 0, remaining = 2,
    )

    override fun observeTaggingQueue(): Flow<SaleTaggingQueueDto?> =
        flowOf(SaleTaggingQueueDto(deals = listOf(SaleTaggingDealDto(salesDealId = "some-other-sale", farm = "CPT", parkId = "park-cpt"))))
    override fun observeTaggingDeal(dealId: String): Flow<SaleTaggingDealDto?> = flowOf(null)
    override suspend fun refreshTaggingDeal(dealId: String): AppResult<SaleTaggingDealDto> = AppResult.Ok(deal)
    override suspend fun saleLocations(): AppResult<SaleLocationsDto> = AppResult.Ok(
        SaleLocationsDto(
            parks = listOf(SaleLocationParkDto("park-cpt", "CPT"), SaleLocationParkDto("park-cbe", "CBE")),
            locations = emptyList<SaleLocationEntryDto>(),
        ),
    )
    override suspend fun saleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, cursor: String?): AppResult<SaleCandidatePageDto> {
        searchedParks += parkId
        return AppResult.Ok(SaleCandidatePageDto(candidates = emptyList()))
    }
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = AppResult.Ok(SaleAllocationDto(salesDealId = dealId))

    override fun deals(farm: String): Flow<PagingData<SalesDealDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeDealScope(farm: String): Flow<SalesDealScopeMeta?> = flowOf(null)
    override suspend fun invalidateDeals(farm: String) = Unit
    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = flowOf(null)
    override fun observeOptions(): Flow<SalesOptionsDto?> = flowOf(null)
    override suspend fun refreshOptions() = Unit
    override fun observeVendorOptions(): Flow<VendorOptionsDto?> = flowOf(null)
    override suspend fun refreshVendorOptions() = Unit
    override suspend fun persistServerDeal(deal: SalesDealDto) = Unit
    override fun buyerLeads(search: String, status: String): Flow<PagingData<SalesBuyerLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun fpoLeads(search: String, status: String): Flow<PagingData<SalesFpoLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeLeadMeta(side: SalesLeadSide, search: String, status: String): Flow<SalesLeadBoardMetaDto?> = flowOf(null)
    override suspend fun refreshLeadMeta(side: SalesLeadSide) = true
    override suspend fun invalidateLeads(side: SalesLeadSide, search: String, status: String) = Unit
    override suspend fun persistServerBuyerLead(lead: SalesBuyerLeadDto) = Unit
    override suspend fun persistServerFpoLead(lead: SalesFpoLeadDto) = Unit
    override suspend fun previewAllocation(request: SaleAllocationRequestDto): AppResult<SalePreviewDto> = error("unused")
    override suspend fun confirmAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): AppResult<SaleAllocationDto> = error("unused")
    override suspend fun refreshTaggingQueue(): AppResult<SaleTaggingQueueDto> = error("unused")
    override suspend fun taggingQueuePage(cursor: String): AppResult<SaleTaggingQueueDto> = error("unused")
}
