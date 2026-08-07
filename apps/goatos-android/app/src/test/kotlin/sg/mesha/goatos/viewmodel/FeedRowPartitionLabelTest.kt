package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto
import sg.mesha.goatos.core.ui.operationalLocationLabel

/**
 * A feed row's grain is the OPERATIONAL LOCATION, not the shed: Castro 1 and Castro 2 are two rows
 * sharing one shed_id, and one shed can run an authored experiment on some partitions while the
 * rest stay on the per-head ration grid.
 *
 * The backend generated those rows correctly, but this client dropped the partition on the floor --
 * the DTO had no field for it -- so the phone received shed_label "Castro" for both and printed two
 * identical lines. The bug was invisible from the API response alone, which is why this pins the
 * WIRE field and the composed label together rather than trusting either half.
 */
class FeedRowPartitionLabelTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `direction row parses partition_label off the wire`() {
        val dto = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"1","workflow":"experiment"}""",
        )
        assertEquals("Castro", dto.shedLabel)
        assertEquals("1", dto.partitionLabel)
    }

    @Test
    fun `packing row parses partition_label off the wire`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"2"}""",
        )
        assertEquals("2", dto.partitionLabel)
    }

    @Test
    fun `a shed with no partitions omits the field entirely`() {
        // The backend emits partition_label with omitempty, so an unpartitioned shed sends no key at
        // all. That must parse as null and render as the bare shed name, not as "Yashoda null".
        val dto = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Yashoda"}""",
        )
        assertNull(dto.partitionLabel)
        assertEquals("Yashoda", operationalLocationLabel(dto.shedLabel, dto.partitionLabel))
    }

    @Test
    fun `two partitions of one shed render as distinct lines`() {
        // THE REGRESSION. Same shed_id, same shed_label, different partitions: if the rendered label
        // is the bare shed name these are two identical rows and the operator cannot tell which pen
        // a bag belongs to.
        val one = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"1"}""",
        )
        val two = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"2"}""",
        )
        val labelOne = operationalLocationLabel(one.shedLabel, one.partitionLabel)
        val labelTwo = operationalLocationLabel(two.shedLabel, two.partitionLabel)

        assertEquals("Castro - 1", labelOne)
        assertEquals("Castro - 2", labelTwo)
        if (labelOne == labelTwo) {
            throw AssertionError("two partitions of one shed rendered identically as $labelOne")
        }
    }

    @Test
    fun `a worded partition keeps its own convention`() {
        val dto = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s2","shed_label":"Godel 2","partition_label":"Part 3"}""",
        )
        assertEquals("Godel 2 - Part 3", operationalLocationLabel(dto.shedLabel, dto.partitionLabel))
    }
}
