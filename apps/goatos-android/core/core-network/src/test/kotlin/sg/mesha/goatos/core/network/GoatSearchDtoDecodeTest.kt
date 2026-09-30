package sg.mesha.goatos.core.network

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.GoatSearchResponseDto

/**
 * An untagged newborn (only its provisional birth tag, CBE-38085) comes back from /goats/search
 * with `"animal_identifier_1": null`. With the app's Json (no null coercion) a non-null String made
 * the WHOLE page fail to decode, so Raise shifting could not find the kid at all -- which is exactly
 * the kid a K0 -> K1 shift task asks to move (Realme E2E 2026-09-30).
 */
class GoatSearchDtoDecodeTest {
    @Test
    fun `an untagged kid decodes with a blank RFID instead of failing the page`() {
        val page = NetworkFactory.json.decodeFromString(
            GoatSearchResponseDto.serializer(),
            """
            {"items":[{"goat_id":"7600d2d1-5b06-47b5-9c83-d90b258e5882","display_id":"G-000002",
              "animal_identifier_1":null,"animal_identifier_2":null,"sex":"female","age_band":null,
              "lifecycle_status":"alive","row_version":1,
              "location_path":{"park_id":"p","shed_id":"s","operational_location_display":"Nursery"}}],
             "next_cursor":null}
            """.trimIndent(),
        )
        assertEquals(1, page.items.size)
        assertEquals("", page.items[0].animalIdentifier1)
        assertEquals("7600d2d1-5b06-47b5-9c83-d90b258e5882", page.items[0].goatId)
    }
}
