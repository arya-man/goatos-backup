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
 * The packing row at the WIRE boundary: one row is ONE PEN-SESSION (maintainer decision 2026-08-11,
 * reverting the 2026-08-10 pen-day row).
 *
 * These parse real response JSON rather than constructing the DTO, because the defect class this
 * guards is a field that exists on the Kotlin type and is never populated from the wire — a
 * "does the property exist" check passes throughout that outage.
 */
class FeedPackingSessionRowTest {

    private val json = Json { ignoreUnknownKeys = true }

    private val morning = """
        {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
         "operational_location_display":"Castro - 2","session_no":1,"session_label":"Morning",
         "workflow":"normal","head_count":40,
         "total_kg":"3.700","status":"ready","lifecycle_status":"pending",
         "items":[{"feed_item":"Maize","quantity_kg":"2.500","status":"resolved"},
                  {"feed_item":"Concentrate","quantity_kg":"1.200","status":"resolved"}]}
    """.trimIndent()

    @Test
    fun `a packing row carries its own session and its own quantities`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(morning)

        assertEquals(1, dto.sessionNo)
        // The authored NAME — the card prints "Morning", never a client-composed "Session 1".
        assertEquals("Morning", dto.sessionLabel)
        assertEquals("3.700", dto.totalKg)
        assertEquals("2.500", dto.items.first { it.feedItem == "Maize" }.quantityKg)
        // A denominator, the same on the pen's sibling session. Never summed across them.
        assertEquals(40L, dto.headCount)
    }

    @Test
    fun `the backend-composed location is used verbatim`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(morning)

        // Required by the contract and once absent from the Go struct entirely, which is how
        // admin-web fell through to a bare "Castro" against all three of Castro's pens.
        assertEquals("Castro - 2", dto.operationalLocationDisplay)
    }

    @Test
    fun `a pen's morning and evening are two distinct rows`() {
        // THE REVERT. Between 2026-08-10 and 2026-08-11 grainKey omitted the session, so a pen's two
        // bags collapsed into one card and one video was asked to prove both. They must be two rows.
        val one = json.decodeFromString<FeedPackingRowDto>(morning)
        val two = json.decodeFromString<FeedPackingRowDto>(
            morning
                .replace(""""session_no":1""", """"session_no":2""")
                .replace(""""session_label":"Morning"""", """"session_label":"Evening""""),
        )

        if (one.grainKey == two.grainKey) {
            throw AssertionError(
                "Castro - 2 morning and evening share packing grainKey ${one.grainKey} — one bag will be lost",
            )
        }
    }

    @Test
    fun `two pens of one shed are still two separate bags`() {
        // Castro 1 and Castro 2 hold different animals on different rations (migration 000137). This
        // has been lost once already and is pinned alongside the session so neither can go again.
        val two = json.decodeFromString<FeedPackingRowDto>(morning)
        val three = json.decodeFromString<FeedPackingRowDto>(
            morning.replace(""""partition_label":"2"""", """"partition_label":"3""""),
        )

        if (two.grainKey == three.grainKey) {
            throw AssertionError("Castro 2 and Castro 3 share packing grainKey ${two.grainKey} — one bag will be lost")
        }
    }

    @Test
    fun `a blocked line carries the gap and never a packable zero`() {
        // A pen can be fine in the morning and short in the evening when the two sessions draw on
        // different feed items. Each is its own row, so the morning stays readable and only the
        // evening reads blocked.
        val blockedEvening = json.decodeFromString<FeedPackingRowDto>(
            """
            {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
             "operational_location_display":"Castro - 2","session_no":2,"session_label":"Evening",
             "workflow":"normal","head_count":40,
             "total_kg":"0.000","status":"blocked","lifecycle_status":"pending",
             "items":[{"feed_item":"Hybrid","status":"blocked",
                       "blocked_reason":{"code":"no_rate","detail":"no authored ration"}}]}
            """.trimIndent(),
        )

        assertEquals("blocked", blockedEvening.status)
        assertEquals("ready", json.decodeFromString<FeedPackingRowDto>(morning).status)
        // A blocked cell carries no quantity -- it is a gap, never a packable zero.
        assertNull(blockedEvening.items.first().quantityKg)
        assertEquals("no authored ration", blockedEvening.items.first().blockedReason?.detail)
    }

    @Test
    fun `the reopened reason travels on the wire`() {
        // The afternoon correction's sentence is the ONLY thing distinguishing a reopened bag from
        // one nobody has packed -- both read lifecycle_status "pending". A DTO that dropped it would
        // leave the operator with two identical cards and no explanation.
        val reopened = json.decodeFromString<FeedPackingRowDto>(
            morning.replace(
                """"lifecycle_status":"pending"""",
                """"lifecycle_status":"pending","rework_reason":"Animals moved in or out of this pen, so the feed quantities changed. Pack the new amounts and record a new video."""",
            ),
        )

        assertEquals(
            "Animals moved in or out of this pen, so the feed quantities changed. " +
                "Pack the new amounts and record a new video.",
            reopened.reworkReason,
        )
    }
}
