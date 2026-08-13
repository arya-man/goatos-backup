package sg.mesha.goatos.viewmodel

import kotlinx.serialization.encodeToString
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
            {"park_name":"CBE","drive_name":"CBE Adult FMD – Jan 2027","drive_total":324,
             "total_count":77,"completed_count":46,
             "total_animals":70,"completed_animals":42,
             "sheds":[
               {"shed_id":"shed-castro","shed_name":"Castro","partition_label":"1",
                "operational_location_display":"Castro 1","total_animals":35},
               {"shed_id":"shed-castro","shed_name":"Castro","partition_label":"2",
                "operational_location_display":"Castro 2","total_animals":35}
             ]}
        """.trimIndent()
        val dto = json.decodeFromString<DriveSummaryDto>(current)
        val summary = dto.toCalendarDriveSummary()
        assertEquals("CBE Adult FMD – Jan 2027", dto.driveName)
        assertEquals(324, dto.driveTotal)
        assertEquals(70, dto.totalAnimals)
        assertEquals(42, dto.completedAnimals)
        assertEquals(listOf("Castro 1", "Castro 2"), summary.locations.map { it.operationalLocationDisplay })
        assertEquals(listOf("1", "2"), summary.locations.map { it.partitionLabel })
        assertEquals(listOf(35, 35), summary.locations.map { it.totalAnimals })
    }

    @Test
    fun mixedVersionDriveShedFallsBackToCanonicalOperationalLocationLabel() {
        val dto = json.decodeFromString<DriveSummaryDto>(
            """{"sheds":[{"shed_id":"shed-godel-1","shed_name":"Godel 1",
                "partition_label":"Part 3","total_animals":12}]}""".trimIndent(),
        )

        assertEquals("Godel 1 - Part 3", dto.toCalendarDriveSummary().locations.single().operationalLocationDisplay)
    }

    // Room cache fidelity for the backend-owned progress contract. CalendarRepository caches the
    // WHOLE response as a JSON blob (CalendarCacheEntity.dtoJson = json.encodeToString(dto)) with the
    // SAME Json config used here, so there is no narrowed columnar copy that could drop the new
    // fields. This pins that: had the cache stored a narrowed row, every cached drive would silently
    // decode progress_* as null and take the LEGACY per-client fallback -- the same parity defect one
    // layer down, and invisible on a warm app.
    @Test
    fun roomJsonBlobRoundTripPreservesProgressContract() {
        val fromApi = json.decodeFromString<DriveSummaryDto>(
            """{"park_name":"CBE","total_count":200,"completed_count":120,
                "total_animals":77,"completed_animals":46,"submitted_animals":60,
                "progress_basis":"animals","progress_completed":46,"progress_total":77,
                "progress_pct":60}""".trimIndent(),
        )
        // Exactly what DefaultCalendarRepository writes into and reads back out of Room.
        val cached = json.decodeFromString<DriveSummaryDto>(json.encodeToString(fromApi))
        assertEquals("animals", cached.progressBasis)
        assertEquals(46, cached.progressCompleted)
        assertEquals(77, cached.progressTotal)
        assertEquals(60, cached.progressPct)
    }

    @Test
    fun cacheRowPredatingTheProgressContractDecodesToNull() {
        val stale = json.decodeFromString<DriveSummaryDto>(
            """{"park_name":"CBE","total_count":200,"completed_count":120,
                "total_animals":77,"completed_animals":46}""".trimIndent(),
        )
        assertNull("a Room row cached before progress_* shipped must fall back, not read 0%", stale.progressPct)
        assertNull(stale.progressCompleted)
        assertNull(stale.progressTotal)
        assertNull(stale.progressBasis)
    }
}
