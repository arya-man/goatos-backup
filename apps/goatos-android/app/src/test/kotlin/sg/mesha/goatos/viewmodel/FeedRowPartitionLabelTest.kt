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

/**
 * The 2026-08-10 merge, at the wire boundary: a packing row is ONE PEN-DAY carrying its sessions.
 *
 * These parse real response JSON rather than constructing the DTO, because the defect class this
 * guards is a field that exists on the Kotlin type and is never populated from the wire — a
 * "does the property exist" check passes throughout that outage.
 */
class FeedPackingSessionBreakdownTest {

    private val json = Json { ignoreUnknownKeys = true }

    private val penDay = """
        {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
         "operational_location_display":"Castro - 2","workflow":"normal","head_count":40,
         "total_kg":"7.400","status":"ready","lifecycle_status":"pending",
         "sessions":[
           {"session_no":1,"session_label":"Morning","total_kg":"3.700","status":"ready",
            "items":[{"feed_item":"Maize","quantity_kg":"2.500","status":"resolved"},
                     {"feed_item":"Concentrate","quantity_kg":"1.200","status":"resolved"}]},
           {"session_no":2,"session_label":"Evening","total_kg":"3.700","status":"ready",
            "items":[{"feed_item":"Maize","quantity_kg":"2.500","status":"resolved"},
                     {"feed_item":"Concentrate","quantity_kg":"1.200","status":"resolved"}]}]}
    """.trimIndent()

    @Test
    fun `a pen-day row carries both sessions with their own quantities`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(penDay)

        assertEquals(2, dto.sessions.size)
        // Authored order and the authored NAME — the card prints "Morning", never "Session 1".
        assertEquals(listOf("Morning", "Evening"), dto.sessions.map { it.sessionLabel })
        assertEquals(listOf(1, 2), dto.sessions.map { it.sessionNo })
        assertEquals("3.700", dto.sessions[0].totalKg)
        assertEquals("2.500", dto.sessions[0].items.first { it.feedItem == "Maize" }.quantityKg)
        // The day total is the sum of the sessions printed above it on the same card.
        assertEquals("7.400", dto.totalKg)
        // Counted once for the day, never per session.
        assertEquals(40L, dto.headCount)
    }

    @Test
    fun `the backend-composed location is used verbatim`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(penDay)

        // Required by the contract and previously absent from the Go struct entirely, which is how
        // admin-web fell through to a bare "Castro" against all three of Castro's pens.
        assertEquals("Castro - 2", dto.operationalLocationDisplay)
    }

    @Test
    fun `the pen-day key no longer varies by session`() {
        // THE MERGE. The old key carried session_no, so one pen produced two bag lines and the
        // operator was asked to film the same work twice. A row's identity is now the pen-day, so a
        // response that still carried a stray session_no cannot split it back into two cards.
        val withStraySession = json.decodeFromString<FeedPackingRowDto>(
            penDay.replace("""{"shed_id":"s1",""", """{"session_no":2,"shed_id":"s1","""),
        )
        val plain = json.decodeFromString<FeedPackingRowDto>(penDay)

        assertEquals(plain.grainKey, withStraySession.grainKey)
    }

    @Test
    fun `two pens of one shed are still two separate bags`() {
        // The merge collapsed the session and MUST NOT have collapsed the pen: Castro 1 and Castro 2
        // hold different animals on different rations (migration 000137).
        val two = json.decodeFromString<FeedPackingRowDto>(penDay)
        val three = json.decodeFromString<FeedPackingRowDto>(
            penDay.replace(""""partition_label":"2"""", """"partition_label":"3""""),
        )

        if (two.grainKey == three.grainKey) {
            throw AssertionError("Castro 2 and Castro 3 share packing grainKey ${two.grainKey} — one bag will be lost")
        }
    }

    @Test
    fun `a blocked evening leaves the morning readable and blocks the day`() {
        // A pen can be fine in the morning and short in the evening when the two sessions draw on
        // different feed items. The DAY must read blocked -- a real gap must not be hidden because
        // the other half happens to be fine -- while each session still reports its own state.
        val mixed = json.decodeFromString<FeedPackingRowDto>(
            """
            {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
             "operational_location_display":"Castro - 2","workflow":"normal","head_count":40,
             "total_kg":"3.700","status":"blocked","lifecycle_status":"pending",
             "sessions":[
               {"session_no":1,"session_label":"Morning","total_kg":"3.700","status":"ready",
                "items":[{"feed_item":"Maize","quantity_kg":"2.500","status":"resolved"}]},
               {"session_no":2,"session_label":"Evening","total_kg":"0.000","status":"blocked",
                "items":[{"feed_item":"Hybrid","status":"blocked",
                          "blocked_reason":{"code":"no_rate","detail":"no authored ration"}}]}]}
            """.trimIndent(),
        )

        assertEquals("blocked", mixed.status)
        // The packer is told WHICH share is short rather than being handed one flag over the day.
        assertEquals("ready", mixed.sessions[0].status)
        assertEquals("blocked", mixed.sessions[1].status)
        // A blocked cell carries no quantity -- it is a gap, never a packable zero.
        assertNull(mixed.sessions[1].items.first().quantityKg)
        assertEquals("no authored ration", mixed.sessions[1].items.first().blockedReason?.detail)
    }
}
