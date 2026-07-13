package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.DriveSummaryDto

// CDR-R1: a drive_summary cached in Room (or returned by an older API instance mid-rollout) BEFORE
// the total_animals/completed_animals fields shipped must decode those fields as NULL -- absent, not
// 0 -- so the card can fall back to the still-present obligation (dose) counts instead of rendering a
// false "0 / 0 animals" over valid legacy data. (Lives in the app module because the core-network
// unit-test source set is pre-existing-broken; DriveSummaryDto is reachable here via the dependency.)
class DriveSummaryDtoDecodeTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun legacyPayloadWithoutAnimalFieldsDecodesToNull() {
        val legacy = """
            {"park_name":"CBE","due_date":"2026-07-18","shed_count":2,"sheds_completed":1,
             "vaccine_labels":["FMD","HS"],"total_count":77,"completed_count":46,
             "remaining_count":31,"due_count":31,"overdue_count":0,"deferred_count":0,
             "owner_label":"PC"}
        """.trimIndent()
        val dto = json.decodeFromString<DriveSummaryDto>(legacy)
        assertNull("total_animals absent on a legacy payload must be null, not 0", dto.totalAnimals)
        assertNull("completed_animals absent on a legacy payload must be null, not 0", dto.completedAnimals)
        // The obligation (dose) counts ARE present and preserved -> the card's fallback has real data.
        assertEquals(77, dto.totalCount)
        assertEquals(46, dto.completedCount)
    }

    @Test
    fun currentPayloadDecodesAnimalFields() {
        val current = """
            {"park_name":"CBE","total_count":77,"completed_count":46,
             "total_animals":70,"completed_animals":42}
        """.trimIndent()
        val dto = json.decodeFromString<DriveSummaryDto>(current)
        assertEquals(70, dto.totalAnimals)
        assertEquals(42, dto.completedAnimals)
    }
}
