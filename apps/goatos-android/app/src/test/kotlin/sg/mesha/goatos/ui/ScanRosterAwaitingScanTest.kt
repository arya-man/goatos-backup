package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.scan.rosterRowsAwaitingScan

/**
 * Pins the confirmed live defect: a shed roster with a "due" (rework/never-scanned) animal must
 * produce a visible list item on the scan screen, not just a bare "1 PENDING" count. The Scan
 * screen's main body previously rendered only [ScanUiState.feed] (done taps) and
 * [ScanUiState.proofActionNeeded] — a roster row that was never scanned had no row anywhere in the
 * main scrollable list, only inside the tap-to-open roster overlay. [rosterRowsAwaitingScan] is the
 * exact selection [sg.mesha.goatos.feature.scan.ScanScreen] now renders inline; this test verifies
 * it surfaces every PENDING roster row and none of the DONE/SKIPPED ones.
 */
class ScanRosterAwaitingScanTest {

    private fun baseState(roster: List<RosterRow>) = ScanUiState(
        shedLabel = "Vaccination · Gandhi 2",
        cohortLabel = "Milking does",
        ringDone = roster.count { it.status == ScanStatus.DONE },
        ringTotal = roster.size,
        ringUnitLabel = "vaccinated",
        tapHint = "",
        vaccineGroups = emptyList(),
        doneCount = roster.count { it.status == ScanStatus.DONE },
        pendingCount = roster.count { it.status == ScanStatus.PENDING },
        skippedCount = roster.count { it.status == ScanStatus.SKIPPED },
        tileLabels = ScanTileLabels(done = "Done", pending = "Pending", skipped = "Skipped"),
        feed = roster.filter { it.status == ScanStatus.DONE }.map {
            ScanFeedEntry(it.primaryTag, it.secondaryTag, it.vaccineLabel, ScanStatus.DONE)
        },
        roster = roster,
        listTitle = "",
        submitLabel = "",
        canSubmit = false,
        scanEnabled = true,
    )

    @Test
    fun `roster with a due row produces a visible awaiting-scan list item`() {
        // Mirrors the live Gandhi 2 Scan defect: 5 animals, 4 completed, 1 sent-back-for-rework
        // reported by the backend as status="due" (see ScanRosterRowDto / repository.go — a
        // rejected completion is deliberately mapped to "due", not a bespoke status).
        val roster = listOf(
            RosterRow("G2-901007000504418", null, "PPR", ScanStatus.DONE, obligationId = "obl-1", goatId = "g1"),
            RosterRow("G2-901007000504332", null, "PPR", ScanStatus.DONE, obligationId = "obl-2", goatId = "g2"),
            RosterRow("G2-901007000504407", null, "PPR", ScanStatus.DONE, obligationId = "obl-3", goatId = "g3"),
            RosterRow("G2-901007000504419", null, "PPR", ScanStatus.PENDING, obligationId = "obl-4", goatId = "g4"),
            RosterRow("G2-901007000504392", null, "PPR", ScanStatus.DONE, obligationId = "obl-5", goatId = "g5"),
        )
        val state = baseState(roster)

        val awaitingScan = state.rosterRowsAwaitingScan()

        assertEquals(
            "the one due animal must be the sole awaiting-scan row",
            listOf("G2-901007000504419"),
            awaitingScan.map { it.primaryTag },
        )
        assertTrue(
            "awaiting-scan rows must never include a completed animal",
            awaitingScan.none { it.status == ScanStatus.DONE },
        )
    }

    @Test
    fun `fully scanned roster produces no awaiting-scan rows`() {
        val roster = listOf(
            RosterRow("TAG-1", null, "PPR", ScanStatus.DONE, obligationId = "obl-1", goatId = "g1"),
            RosterRow("TAG-2", null, "PPR", ScanStatus.SKIPPED, obligationId = "obl-2", goatId = "g2"),
        )

        assertTrue(baseState(roster).rosterRowsAwaitingScan().isEmpty())
    }
}
