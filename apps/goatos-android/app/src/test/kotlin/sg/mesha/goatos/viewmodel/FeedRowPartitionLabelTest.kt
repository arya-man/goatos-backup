package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto
import sg.mesha.goatos.core.ui.operationalLocationLabel

/**
 * A feed row's grain is the exact physical shed. `partition_label` may still appear on older
 * payloads, but the client must not compose it into labels or keys.
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
    fun `legacy partition labels are not composed into display lines`() {
        val one = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"1"}""",
        )
        val two = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s1","shed_label":"Castro","partition_label":"2"}""",
        )
        val labelOne = operationalLocationLabel(one.shedLabel, one.partitionLabel)
        val labelTwo = operationalLocationLabel(two.shedLabel, two.partitionLabel)

        assertEquals("Castro", labelOne)
        assertEquals("Castro", labelTwo)
    }

    @Test
    fun `a worded partition is ignored by client-composed fallback labels`() {
        val dto = json.decodeFromString<FeedDirectionRowDto>(
            """{"shed_id":"s2","shed_label":"Godel 2","partition_label":"Part 3"}""",
        )
        assertEquals("Godel 2", operationalLocationLabel(dto.shedLabel, dto.partitionLabel))
    }
}

/**
 * grainKey is the Room PRIMARY KEY and the LazyColumn item key, so it is IDENTITY, not decoration.
 * Exact shed id is the physical shed identity. During rollout, stale/pre-cutover payloads can still
 * carry parent shed_id plus partition_label; those must stay distinct until the backend sends exact
 * shed ids everywhere.
 */
class FeedRowGrainKeyTest {

    private val json = Json { ignoreUnknownKeys = true }

    private fun direction(partition: String?, shedLabel: String = "Castro", display: String = "") = json.decodeFromString<FeedDirectionRowDto>(
        """{"shed_id":"s1","shed_label":"$shedLabel","workflow":"experiment","ration_group":"Kid",
            "experiment_arm":"Sheep M NEW","shed_tag":"K1","session_no":1
            ${if (display.isBlank()) "" else ""","operational_location_display":"$display""""}
            ${if (partition == null) "" else ""","partition_label":"$partition""""}}""",
    )

    private fun packing(partition: String?, shedLabel: String = "Castro", display: String = "") = json.decodeFromString<FeedPackingRowDto>(
        """{"shed_id":"s1","shed_label":"$shedLabel","workflow":"normal","session_no":1
            ${if (display.isBlank()) "" else ""","operational_location_display":"$display""""}
            ${if (partition == null) "" else ""","partition_label":"$partition""""}}""",
    )

    @Test
    fun `legacy parent shed direction rows keep partition identity during rollout`() {
        val one = direction("1").grainKey
        val two = direction("2").grainKey
        org.junit.Assert.assertNotEquals(one, two)
    }

    @Test
    fun `legacy parent shed packing rows keep partition identity during rollout`() {
        val one = packing("1").grainKey
        val two = packing("2").grainKey
        org.junit.Assert.assertNotEquals(one, two)
    }

    @Test
    fun `exact numbered shed ids do not split on stale partition metadata`() {
        assertEquals(direction("1", shedLabel = "Castro 1").grainKey, direction("Part 1", shedLabel = "Castro 1").grainKey)
        assertEquals(packing("2", shedLabel = "Gandhi 2").grainKey, packing("Part 2", shedLabel = "Gandhi 2").grainKey)
    }

    @Test
    fun `exact part-named shed ids do not split on stale partition metadata`() {
        assertEquals(
            direction("1", shedLabel = "Godel 1 Part 1").grainKey,
            direction("Part 1", shedLabel = "Godel 1 Part 1").grainKey,
        )
        assertEquals(
            packing("Part 3", shedLabel = "Mandela 2 Part 3").grainKey,
            packing("3", shedLabel = "Mandela 2 Part 3").grainKey,
        )
    }

    @Test
    fun `legacy numbered group sheds still keep partition identity`() {
        org.junit.Assert.assertNotEquals(
            direction("1", shedLabel = "Godel 1").grainKey,
            direction("2", shedLabel = "Godel 1").grainKey,
        )
        org.junit.Assert.assertNotEquals(
            packing("1", shedLabel = "Mandela 2").grainKey,
            packing("2", shedLabel = "Mandela 2").grainKey,
        )
    }

    @Test
    fun `legacy parent shed rows can key by backend exact display without exposing partition`() {
        val one = direction("1", display = "Castro 1").grainKey
        val two = direction("2", display = "Castro 2").grainKey

        org.junit.Assert.assertNotEquals(one, two)
        org.junit.Assert.assertTrue(one.contains("|legacy-display|castro 1"))
        org.junit.Assert.assertTrue(two.contains("|legacy-display|castro 2"))
        org.junit.Assert.assertFalse(one.contains("legacy-partition"))
        org.junit.Assert.assertFalse(two.contains("legacy-partition"))
    }

    @Test
    fun `backend exact display does not split on stale partition spelling`() {
        assertEquals(
            direction("2", display = "Castro 2").grainKey,
            direction("Part 2", display = "Castro 2").grainKey,
        )
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
 * The packing row at the wire boundary: one row is one exact shed-session.
 *
 * These parse real response JSON rather than constructing the DTO, because the defect class this
 * guards is a field that exists on the Kotlin type and is never populated from the wire — a
 * "does the property exist" check passes throughout that outage.
 */
class FeedPackingSessionRowTest {

    private val json = Json { ignoreUnknownKeys = true }

    private val morning = """
        {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
         "operational_location_display":"Castro 2","session_no":1,"session_label":"Morning",
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
        // A denominator, the same on the shed's sibling session. Never summed across them.
        assertEquals(40L, dto.headCount)
    }

    @Test
    fun `the backend-composed location is used verbatim`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(morning)

        // Required by the contract and once absent from the Go struct entirely, which is how
        // admin-web fell through to a bare "Castro" against exact Castro shed names.
        assertEquals("Castro 2", dto.operationalLocationDisplay)
    }

    @Test
    fun `a shed's morning and evening are two distinct rows`() {
        // THE REVERT. Between 2026-08-10 and 2026-08-11 grainKey omitted the session, so a shed's two
        // bags collapsed into one card and one video was asked to prove both. They must be two rows.
        val one = json.decodeFromString<FeedPackingRowDto>(morning)
        val two = json.decodeFromString<FeedPackingRowDto>(
            morning
                .replace(""""session_no":1""", """"session_no":2""")
                .replace(""""session_label":"Morning"""", """"session_label":"Evening""""),
        )

        if (one.grainKey == two.grainKey) {
            throw AssertionError(
                "Castro 2 morning and evening share packing grainKey ${one.grainKey} — one bag will be lost",
            )
        }
    }

    @Test
    fun `legacy parent shed partition labels keep packing rows separate until cleanup`() {
        val two = json.decodeFromString<FeedPackingRowDto>(morning)
        val three = json.decodeFromString<FeedPackingRowDto>(
            morning
                .replace(""""partition_label":"2"""", """"partition_label":"3"""")
                .replace(""""operational_location_display":"Castro 2"""", """"operational_location_display":"Castro 3""""),
        )

        assertNotEquals(two.grainKey, three.grainKey)
    }

    @Test
    fun `a blocked line carries the gap and never a packable zero`() {
        // A pen can be fine in the morning and short in the evening when the two sessions draw on
        // different feed items. Each is its own row, so the morning stays readable and only the
        // evening reads blocked.
        val blockedEvening = json.decodeFromString<FeedPackingRowDto>(
            """
            {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
             "operational_location_display":"Castro 2","session_no":2,"session_label":"Evening",
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
