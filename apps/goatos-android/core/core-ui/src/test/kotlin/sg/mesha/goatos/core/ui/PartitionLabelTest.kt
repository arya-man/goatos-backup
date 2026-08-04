package sg.mesha.goatos.core.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The formatter a caller supplies; mirrors the `Part %1$s` string resource. */
private val format: (String) -> String = { "Part $it" }

class PartitionLabelTest {

    @Test
    fun `whole shed drive renders no partition label`() {
        // Regression: this produced "Part whole" on the shed card.
        assertNull(partitionDisplayLabel("whole", format))
        assertNull(partitionDisplayLabel("Whole", format))
        assertNull(partitionDisplayLabel("  whole  ", format))
    }

    @Test
    fun `blank partition renders no label`() {
        assertNull(partitionDisplayLabel("", format))
        assertNull(partitionDisplayLabel("   ", format))
    }

    @Test
    fun `already worded partition is not re-prefixed`() {
        // Regression: "Parts 1-3" does not start with "Part " (it starts with "Parts"), so the old
        // startsWith check missed it and rendered "Part Parts 1-3".
        assertEquals("Parts 1-3", partitionDisplayLabel("Parts 1-3", format))
        assertEquals("Part 3", partitionDisplayLabel("Part 3", format))
        assertEquals("part 3", partitionDisplayLabel("part 3", format))
        assertEquals("Parts 1-5", partitionDisplayLabel("Parts 1-5", format))
    }

    @Test
    fun `bare partition value is worded by the caller`() {
        assertEquals("Part 1", partitionDisplayLabel("1", format))
        assertEquals("Part 3", partitionDisplayLabel("3", format))
        assertEquals("Part A", partitionDisplayLabel("A", format))
    }

    @Test
    fun `a value merely starting with the letters part is still worded`() {
        // "Partial" is not a partition phrase; only "Part"/"Parts" as a whole word are.
        assertEquals("Part Partial", partitionDisplayLabel("Partial", format))
    }

    @Test
    fun `caller supplies localized wording`() {
        assertEquals("भाग 2", partitionDisplayLabel("2") { "भाग $it" })
    }
}
