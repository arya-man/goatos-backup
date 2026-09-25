package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkBoardSummaryDto

/**
 * The My Work tiles count the cards the chips SHOW (maintainer review 2026-09-25: they read the
 * whole board whichever chip was on). The fixture is today's Coimbatore shape: feed cards in
 * progress / done, milk scheduled and in review, preventive care in review and one sent back.
 */
class WorkBoardSelectionTilesTest {
    private val summary = WorkBoardSummaryDto(
        total = 10,
        byLane = mapOf("todo" to 3, "in_progress" to 3, "in_review" to 3, "done" to 1),
        needsAttention = 1,
        byState = mapOf("scheduled" to 3, "in_progress" to 2, "verification_pending" to 3, "rejected" to 1, "completed" to 1),
        byModuleState = mapOf(
            "feed" to mapOf("scheduled" to 1, "in_progress" to 2, "completed" to 1),
            "milk" to mapOf("scheduled" to 2, "verification_pending" to 1),
            "pc_care" to mapOf("verification_pending" to 2, "rejected" to 1),
        ),
    )

    @Test fun noChipCountsTheWholeBoard() {
        assertEquals(WorkBoardTiles(done = 1, pending = 9, needsAttention = 1), selectionTiles(summary, "", ""))
    }

    @Test fun aModuleChipCountsOnlyThatModule() {
        assertEquals(WorkBoardTiles(done = 1, pending = 3, needsAttention = 0), selectionTiles(summary, "feed", ""))
        assertEquals(WorkBoardTiles(done = 0, pending = 3, needsAttention = 1), selectionTiles(summary, "pc_care", ""))
    }

    @Test fun aLaneChipCountsOnlyThatLane() {
        assertEquals(WorkBoardTiles(done = 0, pending = 3, needsAttention = 1), selectionTiles(summary, "", "in_progress"))
        assertEquals(WorkBoardTiles(done = 1, pending = 0, needsAttention = 0), selectionTiles(summary, "", "done"))
    }

    @Test fun bothChipsIntersect() {
        assertEquals(WorkBoardTiles(done = 0, pending = 1, needsAttention = 1), selectionTiles(summary, "pc_care", "in_progress"))
        assertEquals(WorkBoardTiles(done = 0, pending = 0, needsAttention = 0), selectionTiles(summary, "vaccination", ""))
    }

    @Test fun anOlderServerWithoutPerModuleStatesFallsBackToTheWholeBoard() {
        // A module chip with no per-module states falls back to the whole board's states.
        val old = summary.copy(byModuleState = null)
        assertEquals(WorkBoardTiles(done = 1, pending = 9, needsAttention = 1), selectionTiles(old, "feed", ""))
    }
}
