package sg.mesha.goatos.rfid

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto

/**
 * Verifies the debug-only fixture that lets 5 physical RFID cards (fixed sample EPCs, normalized
 * forms of TEMP-CPT-CASTRO1-001..005) stand in for real seeded animals during phone QA. Runs in the
 * debug unit test source set (app/src/testDebug) alongside DebugSampleTagAliaser itself
 * (app/src/debug) — this class does not exist in a release build.
 */
class DebugSampleTagAliaserTest {

    private val activeShedId = "shed-1"
    private val activeTaskId = "task-1"
    private val activePartitionLabel = "Part 1"

    private fun row(goatId: String, tag: String, status: String = "pending"): ScanRosterRowEntity =
        ScanRosterRowEntity(
            id = "$goatId#$tag",
            scopeKey = "unused",
            shedId = activeShedId,
            taskId = activeTaskId,
            goatId = goatId,
            primaryTag = tag,
            secondaryTag = null,
            normalizedPrimaryTag = tag.filter { it.isLetterOrDigit() }.lowercase(),
            normalizedSecondaryTag = null,
            vaccineLabel = "PPR",
            status = status,
            obligationId = "$goatId-obligation",
            seq = 0,
            updatedAt = 0L,
        )

    private fun aliaser(
        activeOpen: List<ScanRosterRowEntity> = emptyList(),
        siblingOpen: List<ScanRosterRowEntity> = emptyList(),
        otherShedOpen: List<ScanRosterRowEntity> = emptyList(),
    ): DebugSampleTagAliaser = DebugSampleTagAliaser(
        FakeExecutionRepository(activeOpen = activeOpen, siblingOpen = siblingOpen, otherShedOpen = otherShedOpen),
    )

    @Test
    fun `sample card 1 maps to first open in-partition animal deterministically`() = runTest {
        val open = listOf(
            row("goat-c", "TAG-C"),
            row("goat-a", "TAG-A"),
            row("goat-b", "TAG-B"),
        )
        val resolver = aliaser(activeOpen = open)

        val resolved = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-001",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[0],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        // Deterministic order = normalizedPrimaryTag ascending: TAG-A, TAG-B, TAG-C -> card 1 = TAG-A.
        assertEquals("taga", resolved)
    }

    @Test
    fun `sample card still maps when scanner path prepends active shed shorthand`() = runTest {
        val resolver = aliaser(activeOpen = listOf(row("goat-a", "TAG-A")))

        val resolved = resolver.resolve(
            rawTag = "G1-TEMP-CPT-CASTRO1-001",
            normalizedTag = "g1${DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[0]}",
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        assertEquals("taga", resolved)
    }

    @Test
    fun `sample cards 4 and 5 map to neighbor partition animals`() = runTest {
        val sibling = listOf(row("goat-x", "TAG-X"), row("goat-y", "TAG-Y"))
        val resolver = aliaser(siblingOpen = sibling)

        val card4 = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-004",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[3],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )
        val card5 = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-005",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[4],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        assertEquals("tagx", card4)
        assertEquals("tagy", card5)
    }

    @Test
    fun `card 4 falls back to a different shed when no sibling partition has open animals`() = runTest {
        val otherShed = listOf(row("goat-z", "TAG-Z"))
        val resolver = aliaser(siblingOpen = emptyList(), otherShedOpen = otherShed)

        val resolved = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-004",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[3],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        assertEquals("tagz", resolved)
    }

    @Test
    fun `repeated scan of the same card returns the same animal within a session`() = runTest {
        val open = listOf(row("goat-a", "TAG-A"), row("goat-b", "TAG-B"))
        val resolver = aliaser(activeOpen = open)

        val first = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-002",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[1],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )
        // Roster now churns (e.g. the previously-first animal got marked done elsewhere) — the cache
        // must still return the SAME animal for card 2, not re-derive from the new open set.
        val second = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-002",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[1],
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        assertEquals(first, second)
        assertEquals("tagb", first)
    }

    @Test
    fun `a non-sample tag passes through untouched`() = runTest {
        val resolver = aliaser(activeOpen = listOf(row("goat-a", "TAG-A")))

        val resolved = resolver.resolve(
            rawTag = "REAL-ANIMAL-TAG-42",
            normalizedTag = "realanimaltag42",
            shedId = activeShedId,
            taskId = activeTaskId,
            partitionLabel = activePartitionLabel,
        )

        assertEquals("realanimaltag42", resolved)
    }

    @Test
    fun `sample tag passes through when there is no roster context`() = runTest {
        val resolver = aliaser(activeOpen = listOf(row("goat-a", "TAG-A")))

        val resolved = resolver.resolve(
            rawTag = "TEMP-CPT-CASTRO1-001",
            normalizedTag = DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[0],
            shedId = activeShedId,
            taskId = null, // free-flow weighing: no task/roster
            partitionLabel = null,
        )

        assertEquals(DebugSampleTagAliaser.SAMPLE_NORMALIZED_TAGS[0], resolved)
    }
}

/** Minimal [ExecutionRepository] fake exercising only the debug-fixture surface; everything else
 *  is `error("unused")` so an accidental call from DebugSampleTagAliaser is loud, not silent. */
private class FakeExecutionRepository(
    private val activeOpen: List<ScanRosterRowEntity>,
    private val siblingOpen: List<ScanRosterRowEntity>,
    private val otherShedOpen: List<ScanRosterRowEntity>,
) : ExecutionRepository {
    override suspend fun openScanRosterRows(shedId: String, taskId: String?, partitionLabel: String?): List<ScanRosterRowEntity> = activeOpen

    override suspend fun siblingPartitionOpenRows(shedId: String, taskId: String?, activePartitionLabel: String?): List<ScanRosterRowEntity> = siblingOpen

    override suspend fun otherShedOpenRows(shedId: String, taskId: String?): List<ScanRosterRowEntity> = otherShedOpen

    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
        includeFilterOptions: Boolean,
    ): VaccinationExecutionResponseDto = error("unused")

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> = error("unused")

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): Result<Unit> =
        error("unused")

    override fun observeScanRosterRows(shedId: String, taskId: String?, windowSize: Int, partitionLabel: String?): Flow<List<ScanRosterRowEntity>> =
        error("unused")

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> = error("unused")

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> = error("unused")

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<ScanRosterRowEntity> = error("unused")

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?, partitionLabel: String?): Result<Unit> = error("unused")

    override suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String, partitionLabel: String?): ScanRosterRowEntity? =
        error("unused")

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): List<StatusCount> = error("unused")

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<StatusCount>> = error("unused")

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<StatusCount> = error("unused")
}
