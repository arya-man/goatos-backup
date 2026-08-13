package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.counts.MilkPreparationStepUi
import sg.mesha.goatos.feature.counts.MilkPreparationUiState

class MilkPreparationQuestionSequenceTest {
    @Test
    fun `goat milk starts unanswered and questions unlock one at a time`() {
        val initial = MilkPreparationUiState()
        assertNull(initial.goatMilkUsed)
        assertTrue(initial.morningQuestionEnabled)
        assertFalse(initial.eveningQuestionEnabled)
        assertFalse(initial.goatMilkQuestionEnabled)

        val afterMorning = initial.copy(morningMilkCollected = "12")
        assertTrue(afterMorning.eveningQuestionEnabled)
        assertFalse(afterMorning.goatMilkQuestionEnabled)

        val afterMilking = afterMorning.copy(eveningMilkCollected = "10")
        assertTrue(afterMilking.goatMilkQuestionEnabled)
    }

    @Test
    fun `preparation step unlocks only after prior answer and video`() {
        val steps = sequenceMilkPreparationSteps(
            listOf(
                MilkPreparationStepUi("goat", "Goat milk", answer = "2", captured = false),
                MilkPreparationStepUi("boil", "Boil"),
            ),
            questionsComplete = true,
        )
        assertTrue(steps[0].enabled)
        assertFalse(steps[1].enabled)

        val advanced = sequenceMilkPreparationSteps(
            steps.mapIndexed { index, step -> if (index == 0) step.copy(captured = true) else step },
            questionsComplete = true,
        )
        assertTrue(advanced[1].enabled)
    }

    @Test
    fun `calculated citric acid step needs video but no numeric answer`() {
        val steps = sequenceMilkPreparationSteps(
            listOf(
                MilkPreparationStepUi(
                    code = "citric_acid_mixing",
                    label = "Add calculated citric acid",
                    requiresAnswer = false,
                    captured = false,
                ),
                MilkPreparationStepUi("next", "Next step"),
            ),
            questionsComplete = true,
        )

        assertTrue(steps[0].enabled)
        assertTrue(steps[0].answerComplete)
        assertFalse(steps[1].enabled)

        val afterVideo = sequenceMilkPreparationSteps(
            steps.mapIndexed { index, step -> if (index == 0) step.copy(captured = true) else step },
            questionsComplete = true,
        )
        assertTrue(afterVideo[1].enabled)
    }

    @Test
    fun `submission snapshots backend calculated citric acid without operator answer`() {
        val state = MilkPreparationUiState(
            morningMilkCollected = "12",
            eveningMilkCollected = "10",
            citricAcidGrams = 301.4,
            steps = listOf(
                MilkPreparationStepUi("uht_milk_quantity", "UHT milk", answer = "47"),
                MilkPreparationStepUi("citric_acid_mixing", "Citric acid", requiresAnswer = false),
            ),
        )

        val answers = milkPreparationAnswers(state)

        assertEquals(47.0, answers.uhtMilkQuantityLitres, 0.0)
        assertEquals(301.4, answers.citricAcidGrams, 0.0)
    }
}
