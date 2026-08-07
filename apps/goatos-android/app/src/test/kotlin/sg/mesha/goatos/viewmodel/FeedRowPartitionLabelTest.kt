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

/**
 * grainKey is the Room PRIMARY KEY and the LazyColumn item key, so it is IDENTITY, not decoration.
 *
 * The shipped defect: it omitted the partition. Castro 1 and Castro 2 share a shed_id and agree on
 * workflow, ration group, arm, tag and session, so they produced the SAME key -- and one silently
 * overwrote the other in the cache. The sheet rendered "Castro - 2" with no Castro 1 anywhere: a
 * DROPPED PEN, which reads as a shed that simply has no feed rather than as an error.
 *
 * Packing was worse: its key was only shedId|workflow|sessionNo, so every partition of a shed
 * collapsed into one bag line.
 */
class FeedRowGrainKeyTest {

    private val json = Json { ignoreUnknownKeys = true }

    private fun direction(partition: String?) = json.decodeFromString<FeedDirectionRowDto>(
        """{"shed_id":"s1","shed_label":"Castro","workflow":"experiment","ration_group":"Kid",
            "experiment_arm":"Sheep M NEW","shed_tag":"K1","session_no":1
            ${if (partition == null) "" else ""","partition_label":"$partition""""}}""",
    )

    private fun packing(partition: String?) = json.decodeFromString<FeedPackingRowDto>(
        """{"shed_id":"s1","shed_label":"Castro","workflow":"normal","session_no":1
            ${if (partition == null) "" else ""","partition_label":"$partition""""}}""",
    )

    @Test
    fun `two partitions of one shed are two distinct direction rows`() {
        val one = direction("1").grainKey
        val two = direction("2").grainKey
        if (one == two) {
            throw AssertionError("Castro 1 and Castro 2 share grainKey $one — one will overwrite the other")
        }
    }

    @Test
    fun `two partitions of one shed are two distinct packing bags`() {
        val one = packing("1").grainKey
        val two = packing("2").grainKey
        if (one == two) {
            throw AssertionError("Castro 1 and Castro 2 share packing grainKey $one — one bag will be lost")
        }
    }

    @Test
    fun `an unpartitioned shed keeps a stable key`() {
        // Absent and blank must agree, or the same shed flips identity between pages and the row
        // duplicates instead of updating.
        assertEquals(direction(null).grainKey, direction("").grainKey)
        assertEquals(packing(null).grainKey, packing("").grainKey)
    }

    @Test
    fun `the same partition is the same row across pages`() {
        assertEquals(direction("Part 3").grainKey, direction("Part 3").grainKey)
        assertEquals(packing("Part 3").grainKey, packing("Part 3").grainKey)
    }
}
