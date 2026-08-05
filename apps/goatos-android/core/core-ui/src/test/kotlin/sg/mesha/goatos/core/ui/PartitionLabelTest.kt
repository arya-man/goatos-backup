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

class OperationalLocationLabelTest {

    @Test
    fun `non-partitioned shed returns bare name`() {
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", null))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", ""))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "whole"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "Whole"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "  whole  "))
    }

    @Test
    fun `numeric partition uses space format`() {
        assertEquals("Castro 2", operationalLocationLabel("Castro", "2"))
        assertEquals("Castro 1", operationalLocationLabel("Castro", "1"))
        assertEquals("Godel 1 5", operationalLocationLabel("Godel 1", "5"))
    }

    @Test
    fun `partition starting with Part uses dash format`() {
        assertEquals("Godel 1 - Part 3", operationalLocationLabel("Godel 1", "Part 3"))
        assertEquals("Yashoda - Part 1", operationalLocationLabel("Yashoda", "Part 1"))
        assertEquals("Yashoda - Parts 1-3", operationalLocationLabel("Yashoda", "Parts 1-3"))
        // Case-insensitive
        assertEquals("Yashoda - part 2", operationalLocationLabel("Yashoda", "part 2"))
    }

    @Test
    fun `null shed name falls back to partition label`() {
        assertEquals("2", operationalLocationLabel(null, "2"))
        assertEquals("Part 3", operationalLocationLabel(null, "Part 3"))
    }

    @Test
    fun `blank shed name falls back to partition label`() {
        assertEquals("2", operationalLocationLabel("", "2"))
        assertEquals("Part 3", operationalLocationLabel("", "Part 3"))
    }

    @Test
    fun `both null or blank returns empty string`() {
        assertEquals("", operationalLocationLabel(null, null))
        assertEquals("", operationalLocationLabel("", ""))
        assertEquals("", operationalLocationLabel(null, ""))
        assertEquals("", operationalLocationLabel("", null))
    }

    @Test
    fun `whitespace is trimmed`() {
        assertEquals("Yashoda 2", operationalLocationLabel("  Yashoda  ", "  2  "))
        assertEquals("Yashoda - Part 3", operationalLocationLabel("  Yashoda  ", "  Part 3  "))
    }

    @Test
    fun `literal whole is treated as non-partitioned regardless of case`() {
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "whole"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "WHOLE"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "WhOlE"))
    }
}
