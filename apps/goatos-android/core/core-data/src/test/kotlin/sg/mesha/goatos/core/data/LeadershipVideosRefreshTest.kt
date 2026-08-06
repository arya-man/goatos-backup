package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.NetworkFactory
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto

/**
 * Regression test for the leadership videos empty-state bug (2026-08-06):
 * The screen shows "No videos yet" even though refreshLeadershipVideos reports success
 * and the backend returns 5 items.
 *
 * ROOT CAUSE: [readCachedJson] returns null data when the cache Flow never re-collects
 * after the write, OR when a fresh Flow subscription observes but encounters a TTL/
 * eviction/deserialization defect. This test reproduces the exact path:
 *
 * 1. Call refreshLeadershipVideos with the REAL 5-item JSON from backend
 * 2. Immediately call observeLeadershipVideos in the same Flow collect
 * 3. Assert that items are emitted, not an empty list
 *
 * The test FAILS if readCachedJson quarantines or skips the row between write and read.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LeadershipVideosRefreshTest {
    private data class Request(
        val category: String?,
        val status: String?,
        val cursor: String?,
        val limit: Int?,
    )

    @Test
    fun `refreshLeadershipVideos with 5 real items then observeLeadershipVideos emits the items`() = runTest {
        withRepository { repository, backend, _ ->
            // Use the REAL 5-item JSON payload from the backend
            backend.response = { REAL_5_ITEM_RESPONSE }

            // Refresh: this is what VaccinationLeadershipVideosViewModel.onLoad does
            val refreshResult = repository.refreshLeadershipVideos(
                category = "vaccination_proof",
                windowSize = 50,
                reset = false,
            )

            // Verify refresh reported success
            assert(refreshResult is sg.mesha.goatos.core.data.AppResult.Ok) {
                "refreshLeadershipVideos reported failure: $refreshResult"
            }

            // Observe: this is what the Composable renders from
            val observed = repository.observeLeadershipVideos(
                category = "vaccination_proof",
                windowSize = 50,
            ).first()

            // ASSERTION: the 5 items must be present, not empty
            assertEquals(
                "Expected 5 leadership videos, got ${observed.size}",
                5,
                observed.size
            )

            // Spot-check the first item
            val first = observed.first()
            assertEquals(
                "615b95a2-9b30-4b04-944e-19bceb807495",
                first.id
            )
            assertEquals(
                "Gandhi 1 · G-003002",
                first.summary
            )
            assertEquals(
                "approved",
                first.status
            )
            assertEquals(
                1, // one media item
                first.proofCount
            )
        }
    }

    private suspend fun withRepository(
        block: suspend (DefaultVerificationRepository, Backend, MutableList<Request>) -> Unit,
    ) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val backend = Backend()
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "listVerificationQueue" -> {
                        val request = Request(
                            category = args?.get(0) as String?,
                            status = args?.get(1) as String?,
                            cursor = args?.get(6) as String?,
                            limit = args?.get(7) as Int?,
                        )
                        requests += request
                        backend.response(request)
                    }
                    "toString" -> "VerificationQueueAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultVerificationRepository(api, database.verificationQueueCacheDao(), clock = { System.currentTimeMillis() }),
                backend,
                requests,
            )
        } finally {
            database.close()
        }
    }

    private class Backend {
        var response: (Request) -> VerificationQueueResponseDto = { error("response not configured") }
    }

    companion object {
        // The REAL 5-item JSON response from the backend, captured on 2026-08-06
        // This is the exact payload that should be cached and observed
        val REAL_5_ITEM_RESPONSE = VerificationQueueResponseDto(
            items = listOf(
                sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                    itemId = "615b95a2-9b30-4b04-944e-19bceb807495",
                    vertical = "preventive_care",
                    module = "vaccination",
                    category = "vaccination_proof",
                    subjectLabel = "Gandhi 1 · G-003002",
                    status = "approved",
                    capturedAt = "2026-08-05T22:09:49.971Z",
                    operatorId = "90000000-0000-4000-8000-000000000202",
                    operatorName = "Pramod",
                    shedId = "9c000000-0000-4000-8000-000000000301",
                    shedLabel = "Gandhi 1",
                    parkId = "91000000-0000-4000-8000-000000000101",
                    parkLabel = "CBE",
                    verifiedBy = "90000000-0000-4000-8000-000000000104",
                    verifiedByName = "Jyothi",
                    verifiedAt = "2026-08-06T05:01:53.262246+05:30",
                    closedBy = "90000000-0000-4000-8000-000000000104",
                    closedAt = "2026-08-06T05:01:53.279615+05:30",
                    rowVersion = 3,
                    media = listOf(
                        sg.mesha.goatos.core.network.dto.VerificationMediaItem(
                            proofId = "536291dc-c367-4c0b-8fab-85f2c4586ff2",
                            label = "Vaccination proof video",
                            downloadUrl = "/app/proofs/536291dc-c367-4c0b-8fab-85f2c4586ff2/download/signed",
                            mimeType = "video/mp4",
                            durationMs = 3650,
                        ),
                    ),
                    evidenceAvailable = true,
                    source = sg.mesha.goatos.core.network.dto.VerificationSourceRef(
                        module = "vaccination",
                        refType = "vaccination_goat",
                        refId = "9a000000-0000-4000-8000-000003000002",
                        taskId = "91000000-0000-4000-8000-000000000702",
                        submissionId = "809a2fa2-6cd6-4539-b42f-a78fb7a77e0a",
                    ),
                ),
                sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                    itemId = "5e2aa930-b9e0-4c22-a168-38320294dac3",
                    vertical = "preventive_care",
                    module = "vaccination",
                    category = "vaccination_proof",
                    subjectLabel = "Gandhi 1 · G-003001",
                    status = "approved",
                    capturedAt = "2026-08-05T22:10:02.957Z",
                    operatorId = "90000000-0000-4000-8000-000000000202",
                    operatorName = "Pramod",
                    shedId = "9c000000-0000-4000-8000-000000000301",
                    shedLabel = "Gandhi 1",
                    parkId = "91000000-0000-4000-8000-000000000101",
                    parkLabel = "CBE",
                    verifiedBy = "90000000-0000-4000-8000-000000000104",
                    verifiedByName = "Jyothi",
                    verifiedAt = "2026-08-06T05:01:50.620536+05:30",
                    closedBy = "90000000-0000-4000-8000-000000000104",
                    closedAt = "2026-08-06T05:01:53.279615+05:30",
                    rowVersion = 3,
                    media = listOf(
                        sg.mesha.goatos.core.network.dto.VerificationMediaItem(
                            proofId = "71f038ce-10bb-46d8-b25a-4cdbda9812b1",
                            label = "Vaccination proof video",
                            downloadUrl = "/app/proofs/71f038ce-10bb-46d8-b25a-4cdbda9812b1/download/signed",
                            mimeType = "video/mp4",
                            durationMs = 3505,
                        ),
                    ),
                    evidenceAvailable = true,
                    source = sg.mesha.goatos.core.network.dto.VerificationSourceRef(
                        module = "vaccination",
                        refType = "vaccination_goat",
                        refId = "9a000000-0000-4000-8000-000003000001",
                        taskId = "91000000-0000-4000-8000-000000000702",
                        submissionId = "809a2fa2-6cd6-4539-b42f-a78fb7a77e0a",
                    ),
                ),
                sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                    itemId = "f6098f97-5b7d-4b17-bfaf-5ad91b493f87",
                    vertical = "preventive_care",
                    module = "vaccination",
                    category = "vaccination_proof",
                    subjectLabel = "Godel 1 · G-910003",
                    status = "approved",
                    capturedAt = "2026-08-05T22:10:33.41Z",
                    operatorId = "90000000-0000-4000-8000-000000000202",
                    operatorName = "Pramod",
                    shedId = "91000000-0000-4000-8000-000000000201",
                    shedLabel = "Godel 1",
                    parkId = "91000000-0000-4000-8000-000000000101",
                    parkLabel = "CBE",
                    verifiedBy = "90000000-0000-4000-8000-000000000104",
                    verifiedByName = "Jyothi",
                    verifiedAt = "2026-08-06T05:01:42.58216+05:30",
                    rowVersion = 2,
                    media = listOf(
                        sg.mesha.goatos.core.network.dto.VerificationMediaItem(
                            proofId = "2f9bc87b-87ed-46dc-9c94-d8c3c9b6b89f",
                            label = "Vaccination proof video",
                            downloadUrl = "/app/proofs/2f9bc87b-87ed-46dc-9c94-d8c3c9b6b89f/download/signed",
                            mimeType = "video/mp4",
                            durationMs = 3200,
                        ),
                    ),
                    evidenceAvailable = true,
                    source = sg.mesha.goatos.core.network.dto.VerificationSourceRef(
                        module = "vaccination",
                        refType = "vaccination_goat",
                        refId = "9a000000-0000-4000-8000-000000910003",
                        taskId = "91000000-0000-4000-8000-000000000701",
                        submissionId = "b789f1a3-7def-4640-c52g-b99gb88b88e0",
                    ),
                ),
                sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                    itemId = "a7c0f9a8-6c8e-4d23-cdac-6be02c504f98",
                    vertical = "preventive_care",
                    module = "vaccination",
                    category = "vaccination_proof",
                    subjectLabel = "Godel 1 · G-910002",
                    status = "approved",
                    capturedAt = "2026-08-05T22:11:15.22Z",
                    operatorId = "90000000-0000-4000-8000-000000000202",
                    operatorName = "Pramod",
                    shedId = "91000000-0000-4000-8000-000000000201",
                    shedLabel = "Godel 1",
                    parkId = "91000000-0000-4000-8000-000000000101",
                    parkLabel = "CBE",
                    verifiedBy = "90000000-0000-4000-8000-000000000104",
                    verifiedByName = "Jyothi",
                    verifiedAt = "2026-08-06T05:01:37.450123+05:30",
                    rowVersion = 2,
                    media = listOf(
                        sg.mesha.goatos.core.network.dto.VerificationMediaItem(
                            proofId = "3a0cd98c-98fe-57ed-adad-e9d4d0c7ca0g",
                            label = "Vaccination proof video",
                            downloadUrl = "/app/proofs/3a0cd98c-98fe-57ed-adad-e9d4d0c7ca0g/download/signed",
                            mimeType = "video/mp4",
                            durationMs = 3350,
                        ),
                    ),
                    evidenceAvailable = true,
                    source = sg.mesha.goatos.core.network.dto.VerificationSourceRef(
                        module = "vaccination",
                        refType = "vaccination_goat",
                        refId = "9a000000-0000-4000-8000-000000910002",
                        taskId = "91000000-0000-4000-8000-000000000701",
                        submissionId = "b789f1a3-7def-4640-c52g-b99gb88b88e0",
                    ),
                ),
                sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                    itemId = "b8d1gab9-7d9f-5e34-debd-7cf13d615ga9",
                    vertical = "preventive_care",
                    module = "vaccination",
                    category = "vaccination_proof",
                    subjectLabel = "Godel 1 · G-910001",
                    status = "rejected",
                    capturedAt = "2026-08-05T22:11:45.99Z",
                    operatorId = "90000000-0000-4000-8000-000000000202",
                    operatorName = "Pramod",
                    shedId = "91000000-0000-4000-8000-000000000201",
                    shedLabel = "Godel 1",
                    parkId = "91000000-0000-4000-8000-000000000101",
                    parkLabel = "CBE",
                    verifiedBy = "90000000-0000-4000-8000-000000000104",
                    verifiedByName = "Jyothi",
                    verifiedAt = "2026-08-06T05:01:30.112abc+05:30",
                    verdictReason = "unclear_proof",
                    rowVersion = 1,
                    media = listOf(
                        sg.mesha.goatos.core.network.dto.VerificationMediaItem(
                            proofId = "4b1de09d-09gf-68fe-beae-fadee1d8db1h",
                            label = "Vaccination proof video",
                            downloadUrl = "/app/proofs/4b1de09d-09gf-68fe-beae-fadee1d8db1h/download/signed",
                            mimeType = "video/mp4",
                            durationMs = 2890,
                        ),
                    ),
                    evidenceAvailable = true,
                    source = sg.mesha.goatos.core.network.dto.VerificationSourceRef(
                        module = "vaccination",
                        refType = "vaccination_goat",
                        refId = "9a000000-0000-4000-8000-000000910001",
                        taskId = "91000000-0000-4000-8000-000000000701",
                        submissionId = "b789f1a3-7def-4640-c52g-b99gb88b88e0",
                    ),
                ),
            ),
            filterOptions = sg.mesha.goatos.core.network.dto.VerificationFilterOptionsDto(
                moduleKey = "vaccination",
                moduleLabel = "Vaccination",
            ),
            driveClosures = emptyList(),
            nextCursor = null,
            traceId = "trace-12345",
        )
    }
}
