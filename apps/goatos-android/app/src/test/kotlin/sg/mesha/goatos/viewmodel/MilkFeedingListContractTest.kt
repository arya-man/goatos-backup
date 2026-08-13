package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkFeedingTaskDto

class MilkFeedingListContractTest {

    @Test
    fun farmSessionsUseMilkPreparationCalendarAndStatusContract() {
        val page = MilkFeedingPageDto(
            feedingDate = "2026-07-30",
            items = listOf(
                MilkFeedingTaskDto("cpt-1", "park-cpt", "CPT", "2026-07-30", 1, "08:00", 55, "not_submitted", available = true),
                MilkFeedingTaskDto("cpt-2", "park-cpt", "CPT", "2026-07-30", 2, "12:00", 55, "pending_verification", available = true),
                MilkFeedingTaskDto("cbe-1", "park-cbe", "CBE", "2026-07-30", 1, "08:00", 99, "rework", available = true),
                MilkFeedingTaskDto("cbe-2", "park-cbe", "CBE", "2026-07-30", 2, "12:00", 99, "completed", available = true),
            ),
        )

        val state = buildMilkFeedingListUi(page, selectedFilter = "all")

        assertEquals("4 farm sessions · 2 need action", state.subtitle)
        assertEquals("Today · 30 Jul", state.dateLabel)
        assertEquals(listOf("all", "not_submitted", "pending_verification", "completed", "rework"), state.chips.map { it.key })
        assertEquals(listOf(4, 1, 1, 1, 1), state.chips.map { it.count })
        assertEquals(listOf("CBE", "CBE", "CPT", "CPT"), state.cards.map { it.parkLabel })
    }

    @Test
    fun sessionBeforeItsBackendUnlockTimeIsVisibleButCannotOpen() {
        val page = MilkFeedingPageDto(
            feedingDate = "2026-07-30",
            items = listOf(
                MilkFeedingTaskDto(
                    taskId = "cpt-4",
                    parkId = "park-cpt",
                    parkLabel = "CPT",
                    feedingDate = "2026-07-30",
                    sessionNo = 4,
                    dueTime = "21:00",
                    available = false,
                    blockedReason = "Available at 21:00",
                ),
            ),
        )

        val card = buildMilkFeedingListUi(page, selectedFilter = "all").cards.single()

        assertFalse(card.canOpen)
        assertEquals("Available at 21:00", card.blockedReason)
    }
}
