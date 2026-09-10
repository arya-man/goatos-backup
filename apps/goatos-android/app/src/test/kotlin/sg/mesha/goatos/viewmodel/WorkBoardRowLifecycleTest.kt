package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkBoardCountsDto
import sg.mesha.goatos.core.network.dto.WorkBoardOwnerDto
import sg.mesha.goatos.core.network.dto.WorkBoardPenDto
import sg.mesha.goatos.core.network.dto.WorkBoardRowDto
import sg.mesha.goatos.feature.workboard.WorkBoardOwnerState
import sg.mesha.goatos.feature.workboard.WorkBoardSeverity

/**
 * A board row's card is the backend row VERBATIM (maintainer decision 2026-09-10), mirroring
 * [FeedDirectionRowLifecycleTest]: `lane`, `work_state`, `severity` and `owner_state` are carried
 * through as the server set them and only ever COLOURED or LABELLED — never re-derived from each
 * other, from the counts, or from the clock.
 */
class WorkBoardRowLifecycleTest {
    @Test
    fun `lane and work state are carried verbatim and never re-derived`() {
        // A rejected row is In progress on the server (back with the operator) even though its
        // counts read as nothing done: the card must not move it to To do or Done on its own.
        val rejected = row(workState = "rejected", lane = "in_progress", done = 0, pending = 3).toRowUi()
        assertEquals("rejected", rejected.workState)
        assertEquals("in_progress", rejected.lane)

        // A completed row whose module still reports pending counts stays Done: the server decided.
        val completed = row(workState = "completed", lane = "done", done = 2, pending = 1).toRowUi()
        assertEquals("done", completed.lane)
        assertEquals(3, completed.total)
    }

    @Test
    fun `severity and owner state map one to one from the wire vocabulary`() {
        assertEquals(WorkBoardSeverity.OK, row(severity = "ok").toRowUi().severity)
        assertEquals(WorkBoardSeverity.WATCH, row(severity = "watch").toRowUi().severity)
        assertEquals(WorkBoardSeverity.AT_RISK, row(severity = "at_risk").toRowUi().severity)
        assertEquals(WorkBoardSeverity.BROKEN, row(severity = "broken").toRowUi().severity)
        // An unknown severity is rendered calm rather than alarming — the words still come from the server.
        assertEquals(WorkBoardSeverity.OK, row(severity = "").toRowUi().severity)

        assertEquals(WorkBoardOwnerState.ASSIGNED, row(ownerState = "assigned").toRowUi().ownerState)
        assertEquals(WorkBoardOwnerState.MISSING, row(ownerState = "missing").toRowUi().ownerState)
        assertEquals(WorkBoardOwnerState.POOL, row(ownerState = "pool").toRowUi().ownerState)
    }

    @Test
    fun `the pen display and every backend sentence pass through untouched`() {
        val ui = row().toRowUi()
        assertEquals("feed|feed_packing_completion|abc", ui.rowKey)
        assertEquals("Pack Godel 1 - Part 3 · Morning", ui.title)
        assertEquals("Godel 1 - Part 3", ui.penLabel)
        assertEquals("Channapatna", ui.parkName)
        assertEquals("Due by 3:00 pm", ui.clockLabel)
        assertEquals("Amit Kumar", ui.ownerName)
        assertEquals("/feed-packing", ui.href)
        assertTrue(ui.href.isNotBlank())
        assertFalse(row(href = "").toRowUi().href.isNotBlank())
    }

    private fun row(
        workState: String = "due",
        lane: String = "todo",
        severity: String = "ok",
        ownerState: String = "assigned",
        done: Int = 1,
        pending: Int = 2,
        href: String = "/feed-packing",
    ): WorkBoardRowDto = WorkBoardRowDto(
        module = "feed",
        sourceType = "feed_packing_completion",
        sourceId = "abc",
        rowKey = "feed|feed_packing_completion|abc",
        parkId = "park-1",
        parkName = "Channapatna",
        pen = WorkBoardPenDto(
            shedId = "shed-1",
            shedName = "Godel 1",
            partitionLabel = "Part 3",
            operationalLocationDisplay = "Godel 1 - Part 3",
        ),
        businessDate = "2026-09-10",
        clockLabel = "Due by 3:00 pm",
        workState = workState,
        lane = lane,
        severity = severity,
        owner = WorkBoardOwnerDto(userId = "u-1", name = "Amit Kumar"),
        ownerState = ownerState,
        title = "Pack Godel 1 - Part 3 · Morning",
        subtitle = "12 kg concentrate",
        counts = WorkBoardCountsDto(done = done, pending = pending, needsAttention = 0),
        href = href,
    )
}
