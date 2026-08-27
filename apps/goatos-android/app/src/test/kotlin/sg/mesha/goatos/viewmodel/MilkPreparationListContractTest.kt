package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.core.network.dto.MilkPreparationFarmTaskDto
import sg.mesha.goatos.core.network.dto.MilkPreparationSummaryDto

class MilkPreparationListContractTest {

    @Test
    fun backendFarmTasksBecomeIndependentBirthDeathStyleWorkCardsAndStatusChips() {
        val page = MilkPreparationPageDto(
            preparationDate = "2026-07-29",
            feedingDate = "2026-07-30",
            farmTasks = listOf(
                MilkPreparationFarmTaskDto(
                    parkId = "park-cpt",
                    parkLabel = "Channapatna",
                    cohortCount = 2,
                    headCount = 52,
                    totalRequiredMl = 51_200,
                    citricAcidGrams = 281.6,
                    verificationStatus = "not_submitted",
                ),
            ),
            summary = MilkPreparationSummaryDto(parkCount = 1, notSubmittedFarmCount = 1),
        )

        val state = buildMilkPreparationListUi(page, selectedFilter = "all")

        assertEquals("1 farm · 1 need action", state.subtitle)
        assertEquals(listOf("all", "not_submitted", "pending_verification", "completed", "rework"), state.chips.map { it.key })
        assertEquals(listOf(1, 1, 0, 0, 0), state.chips.map { it.count })
        assertEquals(listOf("Channapatna"), state.cards.map { it.parkLabel })
        assertTrue(state.cards.first().canOpen)
        assertEquals("Prepare", state.cards.first().actionLabel)
        assertTrue(state.cards.none { it.shedLabel.isNotBlank() })
    }

    @Test
    fun pastPreparationDateRendersReviewOnlyCards() {
        val page = MilkPreparationPageDto(
            preparationDate = "2026-07-29",
            feedingDate = "2026-07-30",
            farmTasks = listOf(
                MilkPreparationFarmTaskDto(
                    parkId = "park-cpt",
                    parkLabel = "Channapatna",
                    cohortCount = 2,
                    headCount = 52,
                    totalRequiredMl = 51_200,
                    citricAcidGrams = 281.6,
                    verificationStatus = "not_submitted",
                ),
            ),
            summary = MilkPreparationSummaryDto(parkCount = 1, notSubmittedFarmCount = 1),
        )

        val state = buildMilkPreparationListUi(page, selectedFilter = "all", draftDate = "2026-07-29")

        assertFalse(state.cards.first().canOpen)
        assertEquals("Prepare", state.cards.first().actionLabel)
    }
}
