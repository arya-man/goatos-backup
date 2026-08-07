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
    fun `numeric partition uses dash format`() {
        assertEquals("Castro - 2", operationalLocationLabel("Castro", "2"))
        assertEquals("Castro - 1", operationalLocationLabel("Castro", "1"))
        assertEquals("Godel 1 - 5", operationalLocationLabel("Godel 1", "5"))
    }

    /**
     * The reason the separator changed (2026-08-06). A shed NAME that itself ends in a digit made
     * the old space form unreadable: "Godel 1" + "1" rendered "Godel 1 1", and "Godel 1" + "10"
     * rendered "Godel 1 10", which cannot be parsed back into shed + partition by eye. This was
     * 98 of 130 live STG destination options (75%), not an edge case.
     */
    @Test
    fun `digit-terminated shed names stay readable`() {
        assertEquals("Godel 1 - 1", operationalLocationLabel("Godel 1", "1"))
        assertEquals("Godel 1 - 10", operationalLocationLabel("Godel 1", "10"))
        assertEquals("Sumathi 2 - 7", operationalLocationLabel("Sumathi 2", "7"))
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
        assertEquals("Yashoda - 2", operationalLocationLabel("  Yashoda  ", "  2  "))
        assertEquals("Yashoda - Part 3", operationalLocationLabel("  Yashoda  ", "  Part 3  "))
    }

    @Test
    fun `literal whole is treated as non-partitioned regardless of case`() {
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "whole"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "WHOLE"))
        assertEquals("Yashoda", operationalLocationLabel("Yashoda", "WhOlE"))
    }
}

/**
 * CANONICAL golden fixture -- the SAME row shapes and expected display strings are pinned in
 * three languages so the same fact can never render three different ways:
 *  - Go:      backend/internal/platform/oploc/golden_fixture_test.go
 *  - TS:      apps/admin-web/lib/operational-location.test.mjs
 *  - Kotlin (this file)
 * Keep row `name` identical across all three files when adding/changing a row.
 */
private data class GoldenFixtureRow(
    val name: String,
    val shedId: String,
    val shedName: String,
    val partitionLabel: String?,
    val want: String,
)

private val goldenFixture = listOf(
    GoldenFixtureRow(
        name = "subdivided shed, numeric-suffixed name, worded partition",
        shedId = "shed-godel-1",
        shedName = "Godel 1",
        partitionLabel = "Part 3",
        want = "Godel 1 - Part 3",
    ),
    GoldenFixtureRow(
        name = "subdivided shed, numeric-suffixed name, two-digit worded partition",
        shedId = "shed-godel-1",
        shedName = "Godel 1",
        partitionLabel = "Part 10",
        want = "Godel 1 - Part 10",
    ),
    GoldenFixtureRow(
        name = "subdivided shed, plain name, bare numeric partition",
        shedId = "shed-castro-cbe",
        shedName = "Castro",
        partitionLabel = "2",
        want = "Castro - 2",
    ),
    GoldenFixtureRow(
        name = "undivided shed, no partition",
        shedId = "shed-yashoda-cbe",
        shedName = "Yashoda",
        partitionLabel = "",
        want = "Yashoda",
    ),
    GoldenFixtureRow(
        name = "'whole' sentinel must never reach the user",
        shedId = "shed-yashoda-cbe",
        shedName = "Yashoda",
        partitionLabel = "whole",
        want = "Yashoda",
    ),
    GoldenFixtureRow(
        name = "empty-string label",
        shedId = "shed-mandela-1",
        shedName = "Mandela 1",
        partitionLabel = "",
        want = "Mandela 1",
    ),
    GoldenFixtureRow(
        name = "NULL label",
        shedId = "shed-mandela-1",
        shedName = "Mandela 1",
        partitionLabel = null,
        want = "Mandela 1",
    ),
    GoldenFixtureRow(
        name = "two same-named sheds, different parks -- CBE",
        shedId = "shed-castro-cbe",
        shedName = "Castro",
        partitionLabel = "1",
        want = "Castro - 1",
    ),
    GoldenFixtureRow(
        name = "two same-named sheds, different parks -- CPT",
        shedId = "shed-castro-cpt",
        shedName = "Castro",
        partitionLabel = "1",
        want = "Castro - 1",
    ),
)

class OperationalLocationGoldenFixtureTest {

    @Test
    fun `canonical cross-surface golden fixture`() {
        for (row in goldenFixture) {
            assertEquals(
                "row '${row.name}'",
                row.want,
                operationalLocationLabel(row.shedName, row.partitionLabel),
            )
        }
    }

    @Test
    fun `same-named sheds in different parks share a display string (shedId is the real key, not exercised by this pure Kotlin helper)`() {
        val cbe = goldenFixture.first { it.name.endsWith("-- CBE") }
        val cpt = goldenFixture.first { it.name.endsWith("-- CPT") }
        assertEquals(
            operationalLocationLabel(cbe.shedName, cbe.partitionLabel),
            operationalLocationLabel(cpt.shedName, cpt.partitionLabel),
        )
        // NOTE: this Android helper is display-only; it has no Key()/grouping equivalent to
        // oploc.Key(). Any Android code that groups/counts by shed must key on shedId, never on
        // this label -- see oploc's TestGoldenFixtureKeyDistinguishesSameNamedShedsAcrossParks.
        assert(cbe.shedId != cpt.shedId)
    }
}
