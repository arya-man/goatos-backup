package sg.mesha.goatos.core.data

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto

/**
 * Pagination-safety test for feed live-status polling: verifies that [fetchDirectionSessionStatus]
 * can find a target row when it appears on page N (not just page 1) by fetching subsequent pages.
 *
 * Context: FeedDirectionCompleteViewModel polls the server periodically to catch a teammate's
 * submission of the same shed-session on another phone. Before this fix, if a park had many
 * sessions or sheds, the target row could fall off page 1 (limit=20), and the poll would miss it,
 * leaving the completion screen editable when it should be locked.
 *
 * The fix: narrow the server-side query to one shed/partition/session, then paginate up to
 * MAX_STATUS_POLL_PAGES if needed.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedDirectionStatusPollPaginationTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun newRepo(api: AppApi): DefaultFeedRepository {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        return DefaultFeedRepository(api, db, db.feedDirectionMetaCacheDao(), db.feedPackingMetaCacheDao(), json)
    }

    private data class FakePage(
        val sheds: List<String>,
        val lifecycleStatuses: Map<String, String> = emptyMap(),
        val hasMore: Boolean = false,
    ) {
        fun toDto(): FeedDirectionPreviewPageDto {
            val items = sheds.mapIndexed { index, shedId ->
                FeedDirectionRowDto(
                    parkId = "park-1",
                    shedId = shedId,
                    partitionLabel = if (shedId == "target-shed") "target-part" else "other-part",
                    sessionNo = 1,
                    lifecycleStatus = lifecycleStatuses[shedId] ?: "pending",
                    // ... other fields defaulted
                )
            }
            return FeedDirectionPreviewPageDto(
                items = items,
                hasMore = hasMore,
                // ... other fields
            )
        }
    }

    private inner class FakePaginatingApi(val pages: List<FakePage>) : AppApi by FakeAppApi() {
        var callCount = 0

        override suspend fun getFeedDirectionPreview(
            parkId: String,
            targetDate: String,
            shedId: String?,
            partitionLabel: String?,
            session: Int?,
            workflow: String?,
            status: String?,
            limit: Int?,
            offset: Int?,
        ): FeedDirectionPreviewPageDto {
            val pageIndex = when {
                offset == null || offset == 0 -> 0
                else -> {
                    // Simulate offset advancing by shed count
                    var shedsSkipped = 0
                    var page = 0
                    for (p in pages) {
                        if (shedsSkipped + p.sheds.size > offset) break
                        shedsSkipped += p.sheds.size
                        page++
                    }
                    page
                }
            }
            callCount++
            check(pageIndex < pages.size) { "offset $offset out of bounds" }
            return pages[pageIndex].toDto()
        }
    }

    @Test
    fun `target row on page 1 is found immediately`() = runTest {
        val api = FakePaginatingApi(
            listOf(
                FakePage(
                    sheds = listOf("other-shed-1", "target-shed", "other-shed-2"),
                    lifecycleStatuses = mapOf("target-shed" to "completed"),
                    hasMore = false,
                ),
            ),
        )

        val repo = newRepo(api)

        val status = repo.fetchDirectionSessionStatus(
            parkId = "park-1",
            shedId = "target-shed",
            partitionLabel = "target-part",
            workflow = "normal",
            sessionNo = 1,
            targetDate = "2026-08-15",
        )

        assertEquals("completed", status)
        assertEquals("Called API once", 1, api.callCount)
    }

    @Test
    fun `target row on page 3 is found after paginating`() = runTest {
        val api = FakePaginatingApi(
            listOf(
                // Page 1: target shed is not here
                FakePage(
                    sheds = listOf("shed-1", "shed-2"),
                    lifecycleStatuses = mapOf("shed-1" to "pending", "shed-2" to "pending"),
                    hasMore = true,
                ),
                // Page 2: target shed is not here
                FakePage(
                    sheds = listOf("shed-3", "shed-4"),
                    lifecycleStatuses = mapOf("shed-3" to "pending", "shed-4" to "pending"),
                    hasMore = true,
                ),
                // Page 3: target shed is found
                FakePage(
                    sheds = listOf("shed-5", "target-shed", "shed-6"),
                    lifecycleStatuses = mapOf("target-shed" to "completed"),
                    hasMore = false,
                ),
            ),
        )

        val repo = newRepo(api)

        val status = repo.fetchDirectionSessionStatus(
            parkId = "park-1",
            shedId = "target-shed",
            partitionLabel = "target-part",
            workflow = "normal",
            sessionNo = 1,
            targetDate = "2026-08-15",
        )

        assertEquals("completed", status)
        assertEquals("Called API three times (one per page)", 3, api.callCount)
    }

    @Test
    fun `target row not found across all pages returns null`() = runTest {
        val api = FakePaginatingApi(
            listOf(
                FakePage(sheds = listOf("shed-1", "shed-2"), hasMore = true),
                FakePage(sheds = listOf("shed-3", "shed-4"), hasMore = true),
                FakePage(sheds = listOf("shed-5", "shed-6"), hasMore = false),
            ),
        )

        val repo = newRepo(api)

        val status = repo.fetchDirectionSessionStatus(
            parkId = "park-1",
            shedId = "target-shed",  // Never appears in pages
            partitionLabel = "target-part",
            workflow = "normal",
            sessionNo = 1,
            targetDate = "2026-08-15",
        )

        assertNull("No match across any page should return null", status)
        assertEquals("Called all 3 pages", 3, api.callCount)
    }

    @Test
    fun `stops paginating after MAX_STATUS_POLL_PAGES even if hasMore`() = runTest {
        // Create 15 pages (more than MAX_STATUS_POLL_PAGES = 10)
        val pages = (0..14).map { i ->
            FakePage(
                sheds = listOf("shed-${i}a", "shed-${i}b"),
                hasMore = i < 14,  // All but last say hasMore=true
            )
        }

        val api = FakePaginatingApi(pages)

        val repo = newRepo(api)

        val status = repo.fetchDirectionSessionStatus(
            parkId = "park-1",
            shedId = "target-shed-never-found",  // Will never match
            partitionLabel = "target-part",
            workflow = "normal",
            sessionNo = 1,
            targetDate = "2026-08-15",
        )

        assertNull("Should give up after MAX_STATUS_POLL_PAGES", status)
        assertEquals("Called exactly MAX_STATUS_POLL_PAGES (10) times", 10, api.callCount)
    }
}
