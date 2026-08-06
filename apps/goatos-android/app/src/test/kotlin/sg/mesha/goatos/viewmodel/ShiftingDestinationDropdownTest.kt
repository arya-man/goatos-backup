package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.feature.counts.ShiftingShedUi

/**
 * Regression test for shed dropdown rendering bugs (2026-08-06).
 *
 * BUG 1: "Record shifting" destination shed dropdown showed "Godel 1 1" for partitions
 * (concatenating parent name with partition number).
 * BUG 2: "Add birth" shed dropdown showed same shed name 6 times for all partitions.
 *
 * Both bugs resulted from using only the `name` field (parent shed name) instead of
 * the backend's `operational_location_display` (properly formatted location string).
 * This test ensures dropdown options are unambiguous and unique.
 */
class ShiftingDestinationDropdownTest {

    @Test
    fun `shed with numeric partitions renders distinct, readable labels`() {
        val park = CountsDestinationParkDto(
            parkId = "park-1",
            name = "Coimbatore",
            sheds = listOf(
                // Godel 1 with 3 numeric partitions
                CountsDestinationShedDto(
                    shedId = "godel-1-shed",
                    name = "Godel 1",
                    partitionLabel = "1",
                    operationalLocationDisplay = "Godel 1 - Part 1",
                ),
                CountsDestinationShedDto(
                    shedId = "godel-1-shed",
                    name = "Godel 1",
                    partitionLabel = "2",
                    operationalLocationDisplay = "Godel 1 - Part 2",
                ),
                CountsDestinationShedDto(
                    shedId = "godel-1-shed",
                    name = "Godel 1",
                    partitionLabel = "10",
                    operationalLocationDisplay = "Godel 1 - Part 10",
                ),
            ),
        )

        val ui = park.toShiftingParkUi()
        val options = ui.sheds

        // All three options present
        assertEquals(3, options.size)

        // Each partition renders a DISTINCT label
        val labels = options.map { it.name }
        assertEquals(listOf("Godel 1 - Part 1", "Godel 1 - Part 2", "Godel 1 - Part 10"), labels)

        // All options have the SAME shed_id but DIFFERENT partition_labels for identity
        assertEquals("godel-1-shed", options[0].shedId)
        assertEquals("godel-1-shed", options[1].shedId)
        assertEquals("godel-1-shed", options[2].shedId)

        assertEquals("1", options[0].partitionLabel)
        assertEquals("2", options[1].partitionLabel)
        assertEquals("10", options[2].partitionLabel)

        // Option keys are stable and unique (used as dropdown selection identifiers)
        val keys = options.map { it.optionKey }
        assertEquals(3, keys.toSet().size)  // All unique
        assertEquals("godel-1-shed|1", keys[0])
        assertEquals("godel-1-shed|2", keys[1])
        assertEquals("godel-1-shed|10", keys[2])
    }

    @Test
    fun `non-partitioned shed renders plain shed name`() {
        val park = CountsDestinationParkDto(
            parkId = "park-1",
            name = "Coimbatore",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = "castro-1-shed",
                    name = "Castro 1",
                    partitionLabel = null,
                    operationalLocationDisplay = "Castro 1",
                ),
            ),
        )

        val ui = park.toShiftingParkUi()
        val option = ui.sheds[0]

        assertEquals("Castro 1", option.name)
        assertNull(option.partitionLabel)
        assertEquals("castro-1-shed", option.optionKey)
    }

    @Test
    fun `falls back to name when operational_location_display is empty (legacy API)`() {
        val park = CountsDestinationParkDto(
            parkId = "park-1",
            name = "Coimbatore",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = "yashoda-shed",
                    name = "Yashoda",
                    partitionLabel = null,
                    operationalLocationDisplay = "",  // Empty, should fall back to name
                ),
            ),
        )

        val ui = park.toShiftingParkUi()
        val option = ui.sheds[0]

        // Falls back to name when operational_location_display is blank
        assertEquals("Yashoda", option.name)
    }

    @Test
    fun `shed with worded partitions (Part 1, Part 2) renders correctly`() {
        val park = CountsDestinationParkDto(
            parkId = "park-1",
            name = "Channapatna",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = "gandhi-1-shed",
                    name = "Gandhi 1",
                    partitionLabel = "Part 1",
                    operationalLocationDisplay = "Gandhi 1 - Part 1",
                ),
                CountsDestinationShedDto(
                    shedId = "gandhi-1-shed",
                    name = "Gandhi 1",
                    partitionLabel = "Part 2",
                    operationalLocationDisplay = "Gandhi 1 - Part 2",
                ),
            ),
        )

        val ui = park.toShiftingParkUi()
        val options = ui.sheds

        assertEquals(2, options.size)
        assertEquals("Gandhi 1 - Part 1", options[0].name)
        assertEquals("Gandhi 1 - Part 2", options[1].name)

        // Options are distinct and unique
        val keys = options.map { it.optionKey }.toSet()
        assertEquals(2, keys.size)
    }

    @Test
    fun `duplicate shed names across parks do not collapse partitions`() {
        val cbe = CountsDestinationParkDto(
            parkId = "cbe-park",
            name = "Coimbatore",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = "cbe-godel-1",
                    name = "Godel 1",
                    partitionLabel = "1",
                    operationalLocationDisplay = "Godel 1 - Part 1",
                ),
            ),
        )
        val cpt = CountsDestinationParkDto(
            parkId = "cpt-park",
            name = "Channapatna",
            sheds = listOf(
                CountsDestinationShedDto(
                    shedId = "cpt-godel-1",
                    name = "Godel 1",
                    partitionLabel = "1",
                    operationalLocationDisplay = "Godel 1 - Part 1",
                ),
            ),
        )

        val cbeOption = cbe.toShiftingParkUi().sheds[0]
        val cptOption = cpt.toShiftingParkUi().sheds[0]

        // Both render the same display text (correct)
        assertEquals("Godel 1 - Part 1", cbeOption.name)
        assertEquals("Godel 1 - Part 1", cptOption.name)

        // But they have different shed_ids (correct identity)
        assertEquals("cbe-godel-1", cbeOption.shedId)
        assertEquals("cpt-godel-1", cptOption.shedId)

        // And their option keys are distinct
        assertNotEquals(cbeOption.optionKey, cptOption.optionKey)
    }

    // Helpers
    private fun assertNull(value: String?) {
        assertEquals(null, value)
    }

    private fun assertNotEquals(expected: String, actual: String) {
        if (expected == actual) {
            throw AssertionError("Values should not be equal. Both are: $expected")
        }
    }
}
