package sg.mesha.goatos.feature.counts

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): the birth form's species and sex choices
 * are the farm's Configuration lists, served beside the breeds; the built-in pair is only the
 * fallback for an older backend or a first open with nothing cached.
 */
class BirthVocabularyOptionsTest {
    private val builtIn = listOf("goat" to "Goat", "sheep" to "Sheep")

    @Test
    fun `the farm's configured species are offered in its order and under its names`() {
        val configured = listOf(
            CountsFilterOptionUi("goat", "Goat", 0),
            CountsFilterOptionUi("sheep", "Sheep", 0),
            CountsFilterOptionUi("alpaca", "Alpaca", 0),
        )
        assertEquals(
            listOf("goat" to "Goat", "sheep" to "Sheep", "alpaca" to "Alpaca"),
            birthVocabularyOptions(configured, builtIn),
        )
    }

    @Test
    fun `nothing served keeps the built-in pair`() {
        assertEquals(builtIn, birthVocabularyOptions(emptyList(), builtIn))
    }

    @Test
    fun `a selection the list no longer offers falls back to its first entry`() {
        val offered = listOf(CountsFilterOptionUi("female", "Female", 0), CountsFilterOptionUi("castrated", "Castrated male", 0))
        assertEquals("castrated", keepOrFirst("castrated", offered))
        assertEquals("female", keepOrFirst("male", offered))
        assertEquals("male", keepOrFirst("male", emptyList()))
    }
}
