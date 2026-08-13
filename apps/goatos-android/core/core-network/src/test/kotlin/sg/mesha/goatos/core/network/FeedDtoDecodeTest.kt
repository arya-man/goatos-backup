package sg.mesha.goatos.core.network

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto

class FeedDtoDecodeTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `feed packing row treats explicit null items as an empty list`() {
        val dto = json.decodeFromString<FeedPackingRowDto>(
            """
            {"shed_id":"s1","shed_label":"Castro","partition_label":"2",
             "operational_location_display":"Castro - 2","session_no":1,"session_label":"Morning",
             "workflow":"normal","head_count":40,
             "total_kg":"0.000","status":"empty","lifecycle_status":"pending",
             "items":null}
            """.trimIndent(),
        )

        assertEquals(0, dto.items.size)
    }
}
