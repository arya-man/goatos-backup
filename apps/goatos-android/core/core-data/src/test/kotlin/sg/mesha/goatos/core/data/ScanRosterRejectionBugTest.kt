package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity

/**
 * Tests for the stale-green bug: when a server rejects a vaccination proof and deletes
 * vaccination_completions, the mobile app must clear its local markers so the animal
 * shows as outstanding again, not GREEN/done.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ScanRosterRejectionBugTest {
    private lateinit var database: GoatDatabase

    @Before
    fun setUp() {
        database = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun `rejected animal removed from observeDoneGoatIds after server sends due status`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "shed-gandhi-1|task-1"

        // Initial state: 5 animals, animal G1 marked as done
        val animals = listOf(
            ScanRosterRowEntity(
                id = "$scopeKey#G1",
                scopeKey = scopeKey,
                shedId = "shed-gandhi-1",
                taskId = "task-1",
                goatId = "G1-901007000504407",
                primaryTag = "TAG-1",
                secondaryTag = null,
                normalizedPrimaryTag = "tag1",
                normalizedSecondaryTag = null,
                vaccineLabel = "PPR",
                status = "done",
                scannedAtMs = 1000L, // Marked as scanned
                obligationId = "obl-1",
                seq = 0L,
                updatedAt = System.currentTimeMillis(),
            ),
            ScanRosterRowEntity(
                id = "$scopeKey#G2",
                scopeKey = scopeKey,
                shedId = "shed-gandhi-1",
                taskId = "task-1",
                goatId = "G2-901007000504408",
                primaryTag = "TAG-2",
                secondaryTag = null,
                normalizedPrimaryTag = "tag2",
                normalizedSecondaryTag = null,
                vaccineLabel = "PPR",
                status = "done",
                scannedAtMs = 1001L, // Marked as scanned
                obligationId = "obl-2",
                seq = 1L,
                updatedAt = System.currentTimeMillis(),
            ),
            ScanRosterRowEntity(
                id = "$scopeKey#G3",
                scopeKey = scopeKey,
                shedId = "shed-gandhi-1",
                taskId = "task-1",
                goatId = "G3-901007000504409",
                primaryTag = "TAG-3",
                secondaryTag = null,
                normalizedPrimaryTag = "tag3",
                normalizedSecondaryTag = null,
                vaccineLabel = "PPR",
                status = "done",
                scannedAtMs = 1002L, // Marked as scanned
                obligationId = "obl-3",
                seq = 2L,
                updatedAt = System.currentTimeMillis(),
            ),
        )
        dao.upsertAll(animals)

        // Verify all 3 animals are marked as done
        var doneGoatIds: List<String> = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals(listOf("G1-901007000504407", "G2-901007000504408", "G3-901007000504409"), doneGoatIds)

        // Simulate server rejection: G1's vaccination_completions is deleted by backend
        // Server now sends status="due" and scannedAt=null for G1
        val rejectedAnimal = animals[0].copy(
            status = "due", // Changed from "done" to "due"
            scannedAtMs = null, // Cleared (was 1000L)
        )
        dao.upsert(rejectedAnimal) // REPLACE the row in the database

        // BUG: If scannedAtMs is not being cleared or if the query has an issue,
        // the rejected animal might still appear as done
        doneGoatIds = dao.observeDoneGoatIds(scopeKey).first()

        // EXPECTED: Only G2 and G3 should be done now, G1 should no longer be done
        // ACTUAL (BUG): G1 might still be in the list if scannedAtMs wasn't cleared or query is wrong
        assertEquals("G1 should no longer be marked as done after rejection",
            listOf("G2-901007000504408", "G3-901007000504409"),
            doneGoatIds
        )

        // The other animals should still be marked as done
        assertEquals("Accepted animals should still be done", 2, doneGoatIds.size)
    }

    @Test
    fun `status update with null scannedAtMs removes animal from done list`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "shed-test|task-test"

        val row = ScanRosterRowEntity(
            id = "$scopeKey#goat-1",
            scopeKey = scopeKey,
            shedId = "shed-test",
            taskId = "task-test",
            goatId = "goat-1",
            primaryTag = "TAG-X",
            secondaryTag = null,
            normalizedPrimaryTag = "tagx",
            normalizedSecondaryTag = null,
            vaccineLabel = "FMD",
            status = "done",
            scannedAtMs = 5000L,
            obligationId = "obl-x",
            seq = 0L,
            updatedAt = System.currentTimeMillis(),
        )

        dao.upsert(row)
        var done: List<String> = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("Initial state: animal should be done", listOf("goat-1"), done)

        // Update: status back to "due", scannedAtMs to null
        val updatedRow = row.copy(status = "due", scannedAtMs = null)
        dao.upsert(updatedRow)

        done = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("After update: animal should no longer be done", emptyList<String>(), done)
    }

    @Test
    fun `mixed vaccine statuses count one open animal not one done and one due`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "gandhi-part-1|vaccination"

        dao.upsertAll(
            listOf(
                ScanRosterRowEntity(
                    id = "$scopeKey#goat-1#blue-tongue",
                    scopeKey = scopeKey,
                    shedId = "gandhi-part-1",
                    taskId = "vaccination",
                    goatId = "goat-1",
                    primaryTag = "TAG-1",
                    secondaryTag = null,
                    normalizedPrimaryTag = "tag1",
                    normalizedSecondaryTag = null,
                    vaccineLabel = "Blue Tongue",
                    status = "completed",
                    scannedAtMs = 1000L,
                    obligationId = "obl-blue-tongue",
                    seq = 0L,
                    updatedAt = 1000L,
                ),
                ScanRosterRowEntity(
                    id = "$scopeKey#goat-1#sheep-pox",
                    scopeKey = scopeKey,
                    shedId = "gandhi-part-1",
                    taskId = "vaccination",
                    goatId = "goat-1",
                    primaryTag = "TAG-1",
                    secondaryTag = null,
                    normalizedPrimaryTag = "tag1",
                    normalizedSecondaryTag = null,
                    vaccineLabel = "Sheep Pox",
                    status = "due",
                    scannedAtMs = null,
                    obligationId = "obl-sheep-pox",
                    seq = 1L,
                    updatedAt = 1000L,
                ),
            ),
        )

        assertEquals(1, dao.observeScopeTotal(scopeKey).first())
        assertEquals(listOf("due" to 1), dao.countByStatus(scopeKey).map { it.status to it.count })
        assertEquals(listOf("due" to 1), dao.observeCountsByStatus(scopeKey).first().map { it.status to it.count })
        assertEquals(listOf("due" to 1), dao.countByStatusForGoats(scopeKey, listOf("goat-1")).map { it.status to it.count })
    }

    @Test
    fun `in progress sibling vaccine keeps the animal open and out of done ids`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "castro-1|vaccination"

        dao.upsertAll(
            listOf(
                ScanRosterRowEntity(
                    id = "$scopeKey#goat-1#et-tt",
                    scopeKey = scopeKey,
                    shedId = "castro-1",
                    taskId = "vaccination",
                    goatId = "goat-1",
                    primaryTag = "TAG-1",
                    secondaryTag = null,
                    normalizedPrimaryTag = "tag1",
                    normalizedSecondaryTag = null,
                    vaccineLabel = "ET+TT",
                    status = "completed",
                    scannedAtMs = 1000L,
                    obligationId = "obl-et-tt",
                    seq = 0L,
                    updatedAt = 1000L,
                ),
                ScanRosterRowEntity(
                    id = "$scopeKey#goat-1#ppr",
                    scopeKey = scopeKey,
                    shedId = "castro-1",
                    taskId = "vaccination",
                    goatId = "goat-1",
                    primaryTag = "TAG-1",
                    secondaryTag = null,
                    normalizedPrimaryTag = "tag1",
                    normalizedSecondaryTag = null,
                    vaccineLabel = "PPR",
                    status = "in_progress",
                    scannedAtMs = 1000L,
                    obligationId = "obl-ppr",
                    seq = 1L,
                    updatedAt = 1000L,
                ),
            ),
        )

        assertEquals(emptyList<String>(), dao.observeDoneGoatIds(scopeKey).first())
        assertEquals(listOf("due" to 1), dao.countByStatus(scopeKey).map { it.status to it.count })
        assertEquals(listOf("due" to 1), dao.observeCountsByStatus(scopeKey).first().map { it.status to it.count })
        assertEquals(listOf("due" to 1), dao.countByStatusForGoats(scopeKey, listOf("goat-1")).map { it.status to it.count })
    }

    @Test
    fun `status stays done if scannedAtMs remains not null`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "shed-stable|task-stable"

        val row = ScanRosterRowEntity(
            id = "$scopeKey#goat-2",
            scopeKey = scopeKey,
            shedId = "shed-stable",
            taskId = "task-stable",
            goatId = "goat-2",
            primaryTag = "TAG-Y",
            secondaryTag = null,
            normalizedPrimaryTag = "tagy",
            normalizedSecondaryTag = null,
            vaccineLabel = "Blue Tongue",
            status = "done",
            scannedAtMs = 6000L,
            obligationId = "obl-y",
            seq = 0L,
            updatedAt = System.currentTimeMillis(),
        )

        dao.upsert(row)
        var done = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("Initial: done", listOf("goat-2"), done)

        // Reupdate with the exact same data - should still be done
        dao.upsert(row)
        done = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("After re-upsert: still done", listOf("goat-2"), done)
    }

    @Test
    fun `shed-wide scan - rejected animal remains due after refreshing without task cleanup`() = runTest {
        // This test documents the bug: when scanning a shed-wide task (no specific taskId),
        // rejected animals' scan records are NOT cleaned up because pruneSyncedFieldToServerDone
        // is only called when taskId is not null/blank. This leaves stale SYNCED scan records
        // that could confuse the UI.
        val dao = database.scanRosterRowDao()
        val scopeKey = "shed-wide-scan|shed-wide" // Shed-wide scope (no taskId)

        val rejectedAnimal = ScanRosterRowEntity(
            id = "$scopeKey#rejected",
            scopeKey = scopeKey,
            shedId = "shed-wide-scan",
            taskId = "shed-wide",
            goatId = "rejected-goat",
            primaryTag = "TAG-R",
            secondaryTag = null,
            normalizedPrimaryTag = "tagr",
            normalizedSecondaryTag = null,
            vaccineLabel = "PPR",
            status = "done",
            scannedAtMs = 2000L,
            obligationId = "obl-rejected",
            seq = 0L,
            updatedAt = System.currentTimeMillis(),
        )

        dao.upsert(rejectedAnimal)
        var done: List<String> = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("Before rejection: animal marked done", listOf("rejected-goat"), done)

        // Simulate server rejection: animal is now due
        val afterRejection = rejectedAnimal.copy(status = "due", scannedAtMs = null)
        dao.upsert(afterRejection)

        done = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("After server rejection: animal should no longer be done", emptyList<String>(), done)
        // NOTE: In a real scenario, shed-wide scans don't trigger the pruneSyncedFieldToServerDone
        // cleanup because authoritativeForTask becomes false when taskId is null.
    }

    @Test
    fun `status marked done by status field alone (no scannedAtMs) is cleared on refresh`() = runTest {
        val dao = database.scanRosterRowDao()
        val scopeKey = "shed-status|task-status"

        // Animal marked done by status field, not scannedAtMs
        val row = ScanRosterRowEntity(
            id = "$scopeKey#goat-3",
            scopeKey = scopeKey,
            shedId = "shed-status",
            taskId = "task-status",
            goatId = "goat-3",
            primaryTag = "TAG-Z",
            secondaryTag = null,
            normalizedPrimaryTag = "tagz",
            normalizedSecondaryTag = null,
            vaccineLabel = "ET+TT",
            status = "completed", // Status says completed, but scannedAtMs is null
            scannedAtMs = null,
            obligationId = "obl-z",
            seq = 0L,
            updatedAt = System.currentTimeMillis(),
        )

        dao.upsert(row)
        var done: List<String> = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("Status-based done", listOf("goat-3"), done)

        // Server sends back status="pending" - no longer done by status
        val updatedRow = row.copy(status = "pending")
        dao.upsert(updatedRow)

        done = dao.observeDoneGoatIds(scopeKey).first()
        assertEquals("After status change: no longer done", emptyList<String>(), done)
    }
}
