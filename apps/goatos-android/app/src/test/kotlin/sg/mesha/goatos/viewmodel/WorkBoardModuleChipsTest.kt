package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The phone shows a chip only for a module that has work on the day (maintainer, 2026-09-25: the
 * 25/09 Coimbatore board read "Health 0" and "Vaccination 0" beside the real ones).
 */
class WorkBoardModuleChipsTest {
    private val modules = listOf("feed", "health", "vaccination", "milk")
    private val byModule = mapOf("feed" to 4, "milk" to 3, "health" to 0)

    @Test
    fun emptyModulesHaveNoChip() {
        val chips = moduleChips(modules, byModule, total = 7, selected = "")
        assertEquals(listOf("", "feed", "milk"), chips.map { it.key })
        assertEquals(listOf(7, 4, 3), chips.map { it.count })
        assertTrue(chips.first().selected)
    }

    @Test
    fun theSelectedChipStaysEvenWhenEmptySoItCanBeCleared() {
        val chips = moduleChips(modules, byModule, total = 7, selected = "health")
        assertEquals(listOf("", "feed", "health", "milk"), chips.map { it.key })
        assertTrue(chips.single { it.key == "health" }.selected)
    }

    @Test
    fun noModulesYetMeansNoChipRow() {
        assertEquals(emptyList<Any>(), moduleChips(emptyList(), emptyMap(), total = 0, selected = ""))
    }
}
