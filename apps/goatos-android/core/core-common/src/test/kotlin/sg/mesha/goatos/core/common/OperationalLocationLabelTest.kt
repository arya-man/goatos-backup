package sg.mesha.goatos.core.common

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Regression for the label an operator read on the weighing schedule on 2026-09-21:
 * "Mandela 1 - Part 1 - Part 1". Every row shape here is a real one from the live register.
 *
 * The Go twin is oploc's TestResolveComposedNameNeverDoublesThePen; the two must agree case for
 * case, because a pen that reads one way on the phone and another on the console is the
 * cross-surface disagreement the weighing rules ban.
 */
class OperationalLocationLabelTest {

    @Test
    fun `a name that already carries the pen is not doubled`() {
        assertEquals("Mandela 1 - Part 1", composeOperationalLocationLabelFromComposedName("Mandela 1 - Part 1", "Part 1"))
        assertEquals("Mandela 1 - Part 10", composeOperationalLocationLabelFromComposedName("Mandela 1 - Part 10", "Part 10"))
        assertEquals("Godel 2 - Part 1", composeOperationalLocationLabelFromComposedName("Godel 2 - Part 1", "Part 1"))
    }

    @Test
    fun `the bare numeric form is caught too`() {
        // The guard this replaced checked only " - Part N", so a park whose canonical shed carries
        // catalog partitions ("Castro" + "1", sent as display_name "Castro 1") still doubled.
        assertEquals("Castro 1", composeOperationalLocationLabelFromComposedName("Castro 1", "1"))
        assertEquals("Gandhi 2", composeOperationalLocationLabelFromComposedName("Gandhi 2", "2"))
    }

    @Test
    fun `a physical shed name still composes normally`() {
        assertEquals("Mandela 1 - Part 3", composeOperationalLocationLabelFromComposedName("Mandela 1", "Part 3"))
        assertEquals("Castro 1", composeOperationalLocationLabelFromComposedName("Castro", "1"))
    }

    @Test
    fun `an undivided shed whose name ends in a digit is never split`() {
        assertEquals("Yashoda 9", composeOperationalLocationLabelFromComposedName("Yashoda 9", null))
        assertEquals("Yashoda", composeOperationalLocationLabelFromComposedName("Yashoda", ""))
    }

    @Test
    fun `the whole sentinel never reaches a screen`() {
        assertEquals("Yashoda", composeOperationalLocationLabelFromComposedName("Yashoda", "whole"))
        assertEquals("Yashoda", composeOperationalLocationLabel("Yashoda", "whole"))
    }

    @Test
    fun `composing twice is a no-op`() {
        for ((name, label) in listOf(
            "Mandela 1 - Part 1" to "Part 1",
            "Castro 1" to "1",
            "Godel 2" to "Part 4",
            "Yashoda 9" to "",
        )) {
            val once = composeOperationalLocationLabelFromComposedName(name, label)
            val twice = composeOperationalLocationLabelFromComposedName(once, label)
            assertEquals("not idempotent for ($name, $label)", once, twice)
        }
    }
}
