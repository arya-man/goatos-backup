package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ExecutionFilterOptionsDto
import sg.mesha.goatos.core.network.dto.ExecutionParkOptionDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.database.capture.CaptureSyncStatus
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import retrofit2.HttpException
import retrofit2.Response
import okhttp3.ResponseBody.Companion.toResponseBody

/**
 * Scan roster is a per-row SSOT ([ScanRosterRowDao]), never a whole-collection JSON blob
 * (docs/decisions/mobile-data-fetch-anti-patterns.md). [DefaultExecutionRepository.refreshScanRoster]
 * walks the WHOLE shed roster page-by-page straight into that table; the UI reads a bounded keyset
 * window, tag lookup + counters + the submit proof-gate resolve the full roster. These tests prove:
 * the whole roster lands in the SSOT with backend `seq` order, the windowed read stays bounded and
 * pages forward LOCALLY (no extra network — page-N works offline), the refresh is atomic (a mid-walk
 * failure leaves the prior roster intact), and no whole-collection blob is written.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ExecutionRepositoryPaginationTest {
    @Test
    fun `card union includes overdue today and legacy memberships but excludes older closed work`() = runTest {
        withRepository { repository, backend, requests ->
            val selectors = listOf(
                ScanRosterSelector(assignmentId = "assignment-overdue", plannedDate = "2026-09-23"),
                ScanRosterSelector(assignmentId = "assignment-today", plannedDate = "2026-09-24"),
                ScanRosterSelector(batchId = "legacy-batch", plannedDate = "2026-09-22"),
            )
            val scope = ScanRosterDateScope("2026-09-24", selectors)
            backend.response = {
                val request = requests.last()
                val member = when {
                    request.assignmentId == "assignment-overdue" && request.plannedDate == "2026-09-23" -> "a"
                    request.assignmentId == "assignment-today" && request.plannedDate == "2026-09-24" -> "b"
                    request.batchId == "legacy-batch" && request.plannedDate == "2026-09-22" -> "c"
                    else -> "closed-unselected"
                }
                ScanRosterResponseDto(rows = listOf(ScanRosterRowDto(goatId = "goat-$member", primaryTag = "TAG-$member",
                    vaccineLabel = "ET+TT", status = "due", obligationId = "obl-$member", taskId = "task-$member")))
            }
            repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, "3", scope.plannedDate, selectors).getOrThrow()
            assertEquals(listOf("goat-a", "goat-b", "goat-c"), repository.observeAssignmentScanRosterRows(SHED_ID, null, null, 20, "3", scope).first().map { it.goatId })
            assertEquals(listOf("task-a", "task-b", "task-c"), repository.observeAssignmentScanRosterTaskIds(SHED_ID, null, null, "3", scope).first())
            assertNull(repository.findAssignmentScanRosterByTag(SHED_ID, null, null, "tagclosedunselected", "3", scope))
            assertEquals(3, requests.size)
            assertEquals(scanRosterRowScopeKey(SHED_ID, null, "3", null, scope), scanRosterRowScopeKey(SHED_ID, null, "3", null, scope.copy(selectors = selectors.reversed())))
            assertEquals(0, repository.observeAssignmentScanRosterTotal(SHED_ID, null, null, "3", scope.copy(selectors = selectors.take(1))).first())
            backend.response = { if (requests.last().assignmentId == "assignment-today") throw IOException("offline second member") else ScanRosterResponseDto(rows = emptyList()) }
            assertTrue(repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, "3", scope.plannedDate, selectors).isFailure)
            assertEquals(3, repository.observeAssignmentScanRosterTotal(SHED_ID, null, null, "3", scope).first())
        }
    }

    @Test
    fun `two dated combined rosters cannot replace or scan each other offline`() = runTest {
        withRepository { repository, backend, _ ->
            val first = ScanRosterDateScope("2026-09-23")
            val second = ScanRosterDateScope("2026-09-24")
            backend.response = { ScanRosterResponseDto(rows = listOf(ScanRosterRowDto(
                goatId = "goat-a", primaryTag = "TAG-A", vaccineLabel = "ET+TT", status = "done",
                obligationId = "obl-a", taskId = "task-a"))) }
            repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, "3", first.plannedDate).getOrThrow()
            backend.response = { throw IOException("offline") }
            assertTrue(repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, "3", second.plannedDate).isFailure)
            assertEquals(0, repository.observeAssignmentScanRosterTotal(SHED_ID, null, null, "3", second).first())
            assertNull(repository.findAssignmentScanRosterByTag(SHED_ID, null, null, "taga", "3", second))
            assertEquals(emptyList<String>(), repository.observeAssignmentScanRosterTaskIds(SHED_ID, null, null, "3", second).first())
            backend.response = { ScanRosterResponseDto(rows = listOf(ScanRosterRowDto(
                goatId = "goat-b", primaryTag = "TAG-B", vaccineLabel = "ET+TT", status = "due",
                obligationId = "obl-b", taskId = "task-b"))) }
            repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, "3", second.plannedDate).getOrThrow()
            assertEquals(listOf("goat-a"), repository.observeAssignmentScanRosterRows(SHED_ID, null, null, 20, "3", first).first().map { it.goatId })
            assertEquals(listOf("goat-b"), repository.observeAssignmentScanRosterRows(SHED_ID, null, null, 20, "3", second).first().map { it.goatId })
            assertEquals(listOf("goat-a"), repository.observeAssignmentScanRosterDoneGoatIds(SHED_ID, null, null, "3", first).first())
            assertEquals(emptyList<String>(), repository.observeAssignmentScanRosterDoneGoatIds(SHED_ID, null, null, "3", second).first())
            assertEquals(emptyList<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>(), repository.assignmentScanRosterRowsByGoatIds(SHED_ID, null, null, listOf("goat-a"), "3", second))
        }
    }

    @Test
    fun `combined roster observes all row task identities outside visible window`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = { cursor ->
                val start = if (cursor == null) 1 else 21
                ScanRosterResponseDto(
                    rows = (start..minOf(start + 19, 21)).map { index ->
                        ScanRosterRowDto(goatId = "goat-$index", primaryTag = "TAG-$index", vaccineLabel = "ET+TT",
                            status = "due", obligationId = "obl-$index", taskId = "task-${index.toString().padStart(2, '0')}")
                    },
                    nextCursor = if (start == 1) "page-2" else null,
                )
            }
            repository.refreshAssignmentScanRoster(SHED_ID, null, null, PAGE_SIZE, "Part 3").getOrThrow()
            assertEquals(20, repository.observeAssignmentScanRosterRows(SHED_ID, null, null, 20, "Part 3").first().size)
            assertEquals((1..21).map { "task-${it.toString().padStart(2, '0')}" }, repository.observeAssignmentScanRosterTaskIds(SHED_ID, null, null, "Part 3").first())
            assertEquals(emptyList<String>(), repository.observeAssignmentScanRosterTaskIds(SHED_ID, null, null, "Part 4").first())
        }
    }

    private data class Request(
        val shedId: String,
        val taskId: String?,
        val cursor: String?,
        val limit: Int?,
        val partitionLabel: String?,
        val assignmentId: String? = null,
        val includeCardSummaries: Boolean? = null,
        val plannedDate: String? = null,
        val batchId: String? = null,
    )

    @Test
    fun `execution continuation merges unique rows and advances cursor`() {
        val first = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-a", "task-a"), executionRow("shed-b", "task-b")),
            totalCount = 3,
            nextCursor = "execution-cursor-1",
        )
        val second = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-b", "task-b"), executionRow("shed-c", "task-c")),
            totalCount = 3,
            nextCursor = null,
        )

        val merged = mergeExecutionRowsPage(first, second)

        assertEquals(listOf("shed-a", "shed-b", "shed-c"), merged.rows.map { it.shedId })
        assertEquals(3, merged.totalCount)
        assertNull(merged.nextCursor)
    }

    @Test
    fun `assignment roster uses assignment scope and empty 200 preserves known rows`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = {
                ScanRosterResponseDto(
                    source = "api",
                    rows = listOf(
                        ScanRosterRowDto(goatId = "goat-1", primaryTag = "TAG-1", vaccineLabel = "ET+TT", status = "due", obligationId = "obl-1"),
                        ScanRosterRowDto(goatId = "goat-2", primaryTag = "TAG-2", vaccineLabel = "ET+TT", status = "due", obligationId = "obl-2"),
                    ),
                )
            }
            repository.refreshAssignmentScanRoster(SHED_ID, TASK_ID, "assignment-1", PAGE_SIZE, "Part 3").getOrThrow()
            assertEquals(2, repository.observeAssignmentScanRosterTotal(SHED_ID, TASK_ID, "assignment-1", "Part 3").first())
            assertEquals("assignment-1", requests.single().assignmentId)

            backend.response = { ScanRosterResponseDto(source = "api", rows = emptyList()) }
            val result = repository.refreshAssignmentScanRoster(SHED_ID, TASK_ID, "assignment-1", PAGE_SIZE, "Part 3")

            assertTrue(result.exceptionOrNull() is EmptyAssignmentRosterException)
            assertEquals(2, repository.observeAssignmentScanRosterTotal(SHED_ID, TASK_ID, "assignment-1", "Part 3").first())
        }
    }

    @Test
    fun `assignment roster 404 never falls back to unscoped shed roster`() = runTest {
        withRepository { repository, backend, requests ->
            backend.taskScopedFailureStatus = 404
            backend.response = ::numberedPage

            val result = repository.refreshAssignmentScanRoster(
                SHED_ID,
                TASK_ID,
                "assignment-1",
                PAGE_SIZE,
                "Part 3",
            )

            assertTrue(result.exceptionOrNull() is HttpException)
            assertEquals(1, requests.size)
            assertEquals("assignment-1", requests.single().assignmentId)
            assertEquals(TASK_ID, requests.single().taskId)
        }
    }

    @Test
    fun `execution continuation preserves first page backend filter options`() {
        val first = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-a", "task-a")),
            totalCount = 2,
            nextCursor = "execution-cursor-1",
            filterOptions = ExecutionFilterOptionsDto(
                parks = listOf(ExecutionParkOptionDto(parkId = "park-a", code = "PA", name = "Park A")),
            ),
        )
        val second = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-b", "task-b")),
            totalCount = 2,
            nextCursor = null,
        )

        val merged = mergeExecutionRowsPage(first, second)

        assertEquals(listOf("park-a"), merged.filterOptions?.parks?.map { it.parkId })
        assertEquals(listOf("shed-a", "shed-b"), merged.rows.map { it.shedId })
    }

    @Test
    fun `execution continuation keeps sibling partitions under one shed`() {
        val first = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-a", "task-a").copy(partitionLabel = "Part 1")),
        )
        val second = VaccinationExecutionResponseDto(
            rows = listOf(executionRow("shed-a", "task-a").copy(partitionLabel = "2")),
        )

        val merged = mergeExecutionRowsPage(first, second)

        assertEquals(listOf("Part 1", "2"), merged.rows.map { it.partitionLabel })
    }

    @Test
    fun `stale execution continuation is ignored as an expected filter race`() = runTest {
        withRepository { repository, backend, requests ->
            backend.executionResponse = { cursor ->
                when (cursor) {
                    null -> VaccinationExecutionResponseDto(
                        rows = listOf(executionRow("shed-a", "task-a")),
                        totalCount = 2,
                        nextCursor = "fresh-cursor",
                    )
                    else -> error("stale cursor must not call backend")
                }
            }

            repository.refreshRows(limit = PAGE_SIZE).getOrThrow()
            val result = repository.appendRows(cursor = "stale-cursor", limit = PAGE_SIZE)

            assertTrue(result.isSuccess)
            assertEquals(listOf(null), requests.map { it.cursor })
            val cached = repository.observeRows(limit = PAGE_SIZE).first().data
            assertEquals(listOf("shed-a"), cached?.rows?.map { it.shedId })
        }
    }

    @Test
    fun `mobile execution rows keep truthful card summaries enabled`() = runTest {
        withRepository { repository, backend, requests ->
            backend.executionResponse = {
                VaccinationExecutionResponseDto(rows = listOf(executionRow("shed-a", "task-a")))
            }

            repository.refreshRows(limit = PAGE_SIZE).getOrThrow()

            assertEquals(listOf(true), requests.map { it.includeCardSummaries })
        }
    }

    @Test
    fun `partition scoped roster requests and caches stay separate`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = { cursor ->
                check(cursor == null)
                ScanRosterResponseDto(
                    source = "api",
                    rows = listOf(ScanRosterRowDto(goatId = "goat-part", primaryTag = "tag-part", status = "due", obligationId = "obl-part")),
                )
            }

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE, partitionLabel = "Part 1").getOrThrow()
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE, partitionLabel = "2").getOrThrow()

            assertEquals(listOf("Part 1", "2"), requests.map { it.partitionLabel })
            assertEquals(1, repository.observeScanRosterTotal(SHED_ID, TASK_ID, "Part 1").first())
            assertEquals(1, repository.observeScanRosterTotal(SHED_ID, TASK_ID, "2").first())
            assertTrue(repository.observeScanRosterTotal(SHED_ID, TASK_ID, "Part 3").first() == 0)
        }
    }

    @Test
    fun `partition scoped shed drilldowns use separate requests and cache entries`() = runTest {
        withRepository { repository, backend, _ ->
            backend.shedResponse = { partitionLabel ->
                VaccinationExecutionShedDrilldownDto(
                    shedId = SHED_ID,
                    shedName = "Castro",
                    partitionLabel = partitionLabel,
                    operationalLocationDisplay = partitionLabel?.let {
                        if (it.startsWith("Part", ignoreCase = true)) "Castro - $it" else "Castro $it"
                    } ?: "Castro",
                )
            }

            repository.refreshShed(SHED_ID, partitionLabel = "Part 1").getOrThrow()
            repository.refreshShed(SHED_ID, partitionLabel = "2").getOrThrow()

            assertEquals(listOf("Part 1", "2"), backend.shedPartitions)
            assertEquals("Part 1", repository.observeShed(SHED_ID, partitionLabel = "Part 1").first().data?.partitionLabel)
            assertEquals("2", repository.observeShed(SHED_ID, partitionLabel = "2").first().data?.partitionLabel)
            assertNull(repository.observeShed(SHED_ID, partitionLabel = "Part 3").first().data)
        }
    }

    @Test
    fun `refresh walks the whole roster into the per-row SSOT ordered by backend seq`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            // Every row landed in the SSOT (full roster), independent of the UI page size.
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(TOTAL_ROWS, repository.getScanRosterStatusCounts(SHED_ID, TASK_ID).sumOf { it.count })
            // The UI list read is a BOUNDED window in backend order (obligation-1..20), not the whole set.
            val window = repository.observeScanRosterRows(SHED_ID, TASK_ID, windowSize = PAGE_SIZE).first()
            assertEquals(PAGE_SIZE, window.size)
            assertEquals((1..PAGE_SIZE).map { "obligation-$it" }, window.map { it.obligationId })
            assertEquals((0L until PAGE_SIZE.toLong()).toList(), window.map { it.seq })
            // A page-3 tag resolves against the full SSOT even though the UI window only shows page 1.
            assertEquals("obligation-45", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag45")?.obligationId)
            // The refresh walked exactly the keyset chain once; no whole-collection blob fetch.
            assertEquals(listOf(null, "cursor-1", "cursor-2"), requests.map { it.cursor })
        }
    }

    @Test
    fun `windowed read pages forward locally after refresh with no extra network (offline)`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            val networkAfterRefresh = requests.size

            // Growing the window reveals page-2 / page-N animals with NO further network — the whole
            // roster is already local, so a scrolling operator (even offline) sees every animal.
            assertEquals(PAGE_SIZE, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE).first().size)
            assertEquals(PAGE_SIZE * 2, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE * 2).first().size)
            val full = repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE * 3).first()
            assertEquals(TOTAL_ROWS, full.size)
            // Page-2 and page-3 animals are present and in order.
            assertEquals("obligation-21", full[20].obligationId)
            assertEquals("obligation-45", full.last().obligationId)
            assertEquals(networkAfterRefresh, requests.size) // ZERO extra network for local paging
        }
    }

    @Test
    fun `refresh is atomic - a mid-walk failure leaves the prior roster intact`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())

            // A later refresh fails on page 2 (offline). The transaction rolls back; the previously
            // persisted full roster must survive so the operator can still scan offline.
            backend.offlineCursor = "cursor-1"
            assertTrue(repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).isFailure)
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals("obligation-45", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag45")?.obligationId)
        }
    }

    @Test
    fun `non-advancing cursor is rejected and the prior roster is intact`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            backend.response = { _ -> ScanRosterResponseDto(source = "api", rows = emptyList(), nextCursor = "cursor-stuck") }
            val result = repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE)
            assertTrue(result.exceptionOrNull() is ScanRosterCursorException)
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
        }
    }

    @Test
    fun `done goat ids and rows-by-goat resolve the full-roster done set for the proof gate`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = { cursor ->
                if (cursor == null) {
                    ScanRosterResponseDto(
                        source = "api",
                        rows = listOf(
                            ScanRosterRowDto(goatId = "goat-1", primaryTag = "tag-1", vaccineLabel = "FMD", status = "done", obligationId = "obl-1"),
                            ScanRosterRowDto(goatId = "goat-2", primaryTag = "tag-2", vaccineLabel = "FMD", status = "due", obligationId = "obl-2"),
                            ScanRosterRowDto(goatId = "goat-3", primaryTag = "tag-3", vaccineLabel = "FMD", status = "completed", obligationId = "obl-3"),
                        ),
                        nextCursor = null,
                    )
                } else {
                    error("single page")
                }
            }
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val done = repository.observeScanRosterDoneGoatIds(SHED_ID, TASK_ID).first().toSet()
            assertEquals(setOf("goat-1", "goat-3"), done)
            val rows = repository.scanRosterRowsByGoatIds(SHED_ID, TASK_ID, listOf("goat-1", "goat-3"))
            assertEquals(setOf("obl-1", "obl-3"), rows.map { it.obligationId }.toSet())
            assertTrue(repository.scanRosterRowsByGoatIds(SHED_ID, TASK_ID, emptyList()).isEmpty())
        }
    }

    @Test
    fun `refresh prunes synced local scan overlay when backend no longer reports completion`() = runTest {
        withRepositoryAndDatabase { repository, backend, _, database ->
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-synced",
                    taskId = TASK_ID,
                    fieldKey = "__scan_roster__",
                    tag = "tag-1",
                    goatId = "goat-1",
                    obligationId = "obl-1",
                    capturedAtMs = 1L,
                    syncStatus = CaptureSyncStatus.SYNCED.name,
                ),
            )
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-pending",
                    taskId = TASK_ID,
                    fieldKey = "__scan_roster__",
                    tag = "tag-2",
                    goatId = "goat-2",
                    obligationId = "obl-2",
                    capturedAtMs = 2L,
                    syncStatus = CaptureSyncStatus.PENDING.name,
                ),
            )
            backend.response = { cursor ->
                if (cursor == null) {
                    ScanRosterResponseDto(
                        source = "api",
                        rows = listOf(
                            ScanRosterRowDto(goatId = "goat-1", primaryTag = "tag-1", vaccineLabel = "FMD", status = "due", obligationId = "obl-1"),
                            ScanRosterRowDto(goatId = "goat-2", primaryTag = "tag-2", vaccineLabel = "FMD", status = "due", obligationId = "obl-2"),
                        ),
                    )
                } else {
                    error("single page")
                }
            }

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val scans = database.scannedGoatDao().listForField(TASK_ID, "whole", "__scan_roster__")
            assertEquals(listOf("scan-pending"), scans.map { it.id })
        }
    }

    @Test
    fun `partition roster refresh never prunes a sibling partition capture`() = runTest {
        withRepositoryAndDatabase { repository, backend, _, database ->
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-part-1",
                    taskId = TASK_ID,
                    partitionKey = "1",
                    fieldKey = "__scan_roster__",
                    tag = "tag-1",
                    goatId = "goat-1",
                    obligationId = "obl-1",
                    capturedAtMs = 1L,
                    syncStatus = CaptureSyncStatus.SYNCED.name,
                ),
            )
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-part-2",
                    taskId = TASK_ID,
                    partitionKey = "2",
                    fieldKey = "__scan_roster__",
                    tag = "tag-2",
                    goatId = "goat-2",
                    obligationId = "obl-2",
                    capturedAtMs = 2L,
                    syncStatus = CaptureSyncStatus.SYNCED.name,
                ),
            )
            backend.response = { cursor ->
                check(cursor == null)
                ScanRosterResponseDto(
                    source = "api",
                    rows = listOf(
                        ScanRosterRowDto(
                            goatId = "goat-1",
                            primaryTag = "tag-1",
                            vaccineLabel = "FMD",
                            status = "due",
                            obligationId = "obl-1",
                        ),
                    ),
                )
            }

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE, partitionLabel = "Part 1").getOrThrow()

            assertTrue(database.scannedGoatDao().listForField(TASK_ID, "1", "__scan_roster__").isEmpty())
            assertEquals(
                listOf("scan-part-2"),
                database.scannedGoatDao().listForField(TASK_ID, "2", "__scan_roster__").map { it.id },
            )
        }
    }

    @Test
    fun `selector card refresh keeps synced unsubmitted scans and never empties a cached roster`() = runTest {
        withRepositoryAndDatabase { repository, backend, _, database ->
            fun scan(member: String) = ScannedGoatEntity(
                id = "scan-$member", taskId = "task-$member", fieldKey = "__scan_roster__", tag = "tag-$member",
                goatId = "goat-$member", obligationId = "obl-$member", capturedAtMs = 1L,
                syncStatus = CaptureSyncStatus.SYNCED.name,
            )
            listOf("a", "b", "c").forEach { database.scannedGoatDao().insert(scan(it)) }
            // a: row read without task identity (no capture visibility) -> outstanding status is NOT authoritative.
            // b: server sees the synced capture on the row's own task -> done.
            // c: row carries its task and is outstanding -> genuinely sent back, prune.
            backend.response = { ScanRosterResponseDto(rows = listOf(
                ScanRosterRowDto(goatId = "goat-a", primaryTag = "tag-a", vaccineLabel = "ET", status = "due", obligationId = "obl-a"),
                ScanRosterRowDto(goatId = "goat-b", primaryTag = "tag-b", vaccineLabel = "ET", status = "done", obligationId = "obl-b", taskId = "task-b"),
                ScanRosterRowDto(goatId = "goat-c", primaryTag = "tag-c", vaccineLabel = "ET", status = "due", obligationId = "obl-c", taskId = "task-c"),
            )) }
            for (selectors in listOf(
                listOf(ScanRosterSelector(assignmentId = "assignment-1", plannedDate = "2026-09-24")),
                listOf(
                    ScanRosterSelector(assignmentId = "assignment-1", plannedDate = "2026-09-24"),
                    ScanRosterSelector(batchId = "batch-2", plannedDate = "2026-09-23"),
                ),
            )) {
                repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, null, "2026-09-24", selectors).getOrThrow()
            }
            assertEquals(listOf("scan-a"), database.scannedGoatDao().listForField("task-a", "whole", "__scan_roster__").map { it.id })
            assertEquals(listOf("scan-b"), database.scannedGoatDao().listForField("task-b", "whole", "__scan_roster__").map { it.id })
            assertTrue(database.scannedGoatDao().listForField("task-c", "whole", "__scan_roster__").isEmpty())

            val selectors = listOf(ScanRosterSelector(assignmentId = "assignment-1", plannedDate = "2026-09-24"))
            val scope = ScanRosterDateScope("2026-09-24", selectors)
            assertEquals(3, repository.observeAssignmentScanRosterTotal(SHED_ID, null, null, null, scope).first())
            backend.response = { ScanRosterResponseDto(rows = emptyList()) }
            assertTrue(repository.refreshAssignmentScanRosterForDate(SHED_ID, null, null, PAGE_SIZE, null, "2026-09-24", selectors).isFailure)
            assertEquals(3, repository.observeAssignmentScanRosterTotal(SHED_ID, null, null, null, scope).first())
        }
    }

    @Test
    fun `refresh keeps synced local scan overlay that backend still reports done`() = runTest {
        withRepositoryAndDatabase { repository, backend, _, database ->
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-synced",
                    taskId = TASK_ID,
                    fieldKey = "__scan_roster__",
                    tag = "tag-1",
                    goatId = "goat-1",
                    obligationId = "obl-1",
                    capturedAtMs = 1L,
                    syncStatus = CaptureSyncStatus.SYNCED.name,
                ),
            )
            backend.response = { cursor ->
                if (cursor == null) {
                    ScanRosterResponseDto(
                        source = "api",
                        rows = listOf(
                            ScanRosterRowDto(goatId = "goat-1", primaryTag = "tag-1", vaccineLabel = "FMD", status = "done", obligationId = "obl-1"),
                        ),
                    )
                } else {
                    error("single page")
                }
            }

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val scans = database.scannedGoatDao().listForField(TASK_ID, "whole", "__scan_roster__")
            assertEquals(listOf("scan-synced"), scans.map { it.id })
        }
    }

    @Test
    fun `shed-wide and task-scoped rosters keep separate SSOT scopes`() = runTest {
        withRepository { repository, backend, _ ->
            backend.response = ::numberedPage
            repository.refreshScanRoster(SHED_ID, taskId = null, limit = PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, taskId = null).first())
            // The task scope is still empty until its own refresh.
            assertEquals(0, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, taskId = null).first())
        }
    }

    @Test
    fun `task-scoped roster falls back to shed roster but publishes into task scope`() = runTest {
        withRepository { repository, backend, requests ->
            backend.taskScopedFailureStatus = 404
            backend.response = ::numberedPage

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            assertEquals(TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(0, repository.observeScanRosterTotal(SHED_ID, taskId = null).first())
            assertEquals(
                listOf(TASK_ID, null, null, null),
                requests.map { it.taskId },
            )
        }
    }

    @Test
    fun `task-scoped roster fallback does not prune synced local scan evidence`() = runTest {
        withRepositoryAndDatabase { repository, backend, _, database ->
            database.scannedGoatDao().insert(
                ScannedGoatEntity(
                    id = "scan-synced",
                    taskId = TASK_ID,
                    fieldKey = "__scan_roster__",
                    tag = "tag-1",
                    goatId = "goat-1",
                    obligationId = "obl-1",
                    capturedAtMs = 1L,
                    syncStatus = CaptureSyncStatus.SYNCED.name,
                ),
            )
            backend.taskScopedFailureStatus = 404
            backend.response = { cursor ->
                if (cursor == null) {
                    ScanRosterResponseDto(
                        source = "api",
                        rows = listOf(
                            ScanRosterRowDto(goatId = "goat-1", primaryTag = "tag-1", vaccineLabel = "FMD", status = "due", obligationId = "obl-1"),
                        ),
                    )
                } else {
                    error("single page")
                }
            }

            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            val scans = database.scannedGoatDao().listForField(TASK_ID, "whole", "__scan_roster__")
            assertEquals(listOf("scan-synced"), scans.map { it.id })
        }
    }

    @Test
    fun `task-scoped roster non-404 failure does not fall back to shed roster`() = runTest {
        withRepository { repository, backend, requests ->
            backend.taskScopedFailureStatus = 500
            backend.response = ::numberedPage

            val result = repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE)

            assertTrue(result.exceptionOrNull() is HttpException)
            assertEquals(0, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            assertEquals(listOf(TASK_ID), requests.map { it.taskId })
        }
    }

    @Test
    fun `large multi-page roster streams every page into the SSOT with a bounded window`() = runTest {
        withRepository { repository, backend, requests ->
            backend.response = ::largeNumberedPage
            repository.refreshScanRoster(SHED_ID, TASK_ID, PAGE_SIZE).getOrThrow()

            // The UI window stays bounded to one page even though the SSOT holds all 80 rows.
            assertEquals(PAGE_SIZE, repository.observeScanRosterRows(SHED_ID, TASK_ID, PAGE_SIZE).first().size)
            assertEquals(LARGE_TOTAL_ROWS, repository.observeScanRosterTotal(SHED_ID, TASK_ID).first())
            // Page-3 and page-4 animals are present in the SSOT for tag lookup.
            assertEquals("obligation-55", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag55")?.obligationId)
            assertEquals("obligation-80", repository.findScanRosterByTag(SHED_ID, TASK_ID, "tag80")?.obligationId)
            assertEquals(listOf(null, "cursor-1", "cursor-2", "cursor-3"), requests.map { it.cursor })
        }
    }

    private suspend fun withRepository(
        block: suspend (DefaultExecutionRepository, Backend, MutableList<Request>) -> Unit,
    ) = withRepositoryAndDatabase { repository, backend, requests, _ ->
        block(repository, backend, requests)
    }

    private suspend fun withRepositoryAndDatabase(
        block: suspend (DefaultExecutionRepository, Backend, MutableList<Request>, GoatDatabase) -> Unit,
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
                    "getScanRoster", "getScanRosterForDate", "getScanRosterForSelection" -> {
                        val request = Request(
                            shedId = args?.get(0) as String,
                            taskId = args[1] as String?,
                            cursor = args[2] as String?,
                            limit = args[3] as Int?,
                            partitionLabel = args[4] as String?,
                            assignmentId = args[5] as String?,
                            plannedDate = if (method.name != "getScanRoster") args[6] as String? else null,
                            batchId = if (method.name == "getScanRosterForSelection") args[7] as String? else null,
                        )
                        requests += request
                        if (backend.offlineCursor != null && backend.offlineCursor == request.cursor) {
                            throw IOException("offline")
                        }
                        backend.taskScopedFailureStatus?.let { status ->
                            if (request.taskId != null) {
                                throw HttpException(Response.error<Unit>(status, "".toResponseBody(null)))
                            }
                        }
                        backend.response(request.cursor)
                    }
                    "listVaccinationExecution" -> {
                        val request = Request(
                            shedId = args?.get(0) as String? ?: "execution",
                            taskId = args?.get(1) as String?,
                            cursor = args?.get(6) as String?,
                            limit = args?.get(5) as Int?,
                            partitionLabel = null,
                            includeCardSummaries = args?.get(8) as Boolean?,
                        )
                        requests += request
                        backend.executionResponse(request.cursor)
                    }
                    "getVaccinationExecutionShed" -> {
                        val partitionLabel = args?.get(4) as String?
                        backend.shedPartitions += partitionLabel
                        backend.shedResponse(partitionLabel)
                    }
                    "toString" -> "ScanRosterAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            block(
                DefaultExecutionRepository(
                    api,
                    database.executionRowsCacheDao(),
                    database.executionShedCacheDao(),
                    database.scanRosterRowDao(),
                    database,
                    clock = { 42L },
                ),
                backend,
                requests,
                database,
            )
        } finally {
            database.close()
        }
    }

    private class Backend {
        var offlineCursor: String? = null
        var taskScopedFailureStatus: Int? = null
        var response: (String?) -> ScanRosterResponseDto = { error("response not configured") }
        var executionResponse: (String?) -> VaccinationExecutionResponseDto = { error("execution response not configured") }
        val shedPartitions = mutableListOf<String?>()
        var shedResponse: (String?) -> VaccinationExecutionShedDrilldownDto = { error("shed response not configured") }
    }

    private fun numberedPage(cursor: String?): ScanRosterResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val rows = (start..TOTAL_ROWS).take(PAGE_SIZE).map { index ->
            ScanRosterRowDto(
                goatId = "goat-$index",
                primaryTag = "tag-$index",
                vaccineLabel = "FMD",
                status = "due",
                obligationId = "obligation-$index",
            )
        }
        val next = if (start + rows.size - 1 < TOTAL_ROWS) "cursor-${pageIndex + 1}" else null
        return ScanRosterResponseDto(source = "api", rows = rows, nextCursor = next)
    }

    private fun largeNumberedPage(cursor: String?): ScanRosterResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val rows = (start..LARGE_TOTAL_ROWS).take(PAGE_SIZE).map { index ->
            ScanRosterRowDto(
                goatId = "goat-$index",
                primaryTag = "tag$index",
                vaccineLabel = "FMD",
                status = "due",
                obligationId = "obligation-$index",
            )
        }
        val next = if (start + rows.size - 1 < LARGE_TOTAL_ROWS) "cursor-${pageIndex + 1}" else null
        return ScanRosterResponseDto(source = "api", rows = rows, nextCursor = next)
    }

    private fun executionRow(shedId: String, taskId: String) = VaccinationExecutionRowDto(
        parkId = "park-a",
        shedId = shedId,
        shedName = shedId,
        animalStage = "K1",
        sopTaskId = taskId,
    )

    private companion object {
        const val SHED_ID = "shed-a"
        const val TASK_ID = "task-a"
        const val PAGE_SIZE = 20
        const val TOTAL_ROWS = 45
        const val LARGE_TOTAL_ROWS = 80
    }
}
