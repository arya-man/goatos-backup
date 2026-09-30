package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.PcCareTaskDto

/**
 * A task detail carries its PINNED card (`sop`), and the card's captures are the AUTHORED shape
 * (key / title / hint / kind / required / min_seconds). Found on a real phone 2026-09-30: the DTO
 * typed them as task slots (field_key / label), so every detail carrying its card failed to decode
 * and a fumigation task opened on an empty screen. The JSON below is the server's own output shape.
 */
class PcCareTaskDetailDecodeTest {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    @Test
    fun `a task detail with its pinned card decodes and keeps the pen captures`() {
        val body = """
            {"task_id":"t1","category":"fumigation","park_id":"p","park_label":"Channapatna","shed_id":"s",
             "shed_label":"Castro","partition_label":"1","planned_business_date":"2026-09-30",
             "due_business_date":"2026-09-30","work_state":"scheduled","status":"open","row_version":1,
             "capture_mode":"task_proof",
             "expected_slots":[
               {"field_key":"mixing_video","label":"Mixing video","kind":"video","required":true},
               {"field_key":"spraying_video","label":"Spraying video","kind":"video","required":true}],
             "sop":{"version":2,
               "feed_water_removal":{"mode":"optional","applies_to":["deworming"],"proofs":[
                 {"key":"feed_video","title":"Feed removal video","kind":"video","required":true}],"questions":[]},
               "categories":{
                 "hoof_trimming":{"instruction":"Pick each animal","proofs":[
                   {"key":"during_video","title":"While trimming","hint":"Record","kind":"video","required":true,"min_seconds":10}],"questions":[]},
                 "fumigation":{"instruction":"Mix 5 ml of Virufix liquid into every litre of water and spray the whole pen.","proofs":[
                   {"key":"mixing_video","title":"Mixing video","kind":"video","required":true},
                   {"key":"spraying_video","title":"Spraying video","kind":"video","required":true},
                   {"key":"sprayed_pen_photo","title":"Sprayed pen photo","kind":"photo","required":true}],"questions":[]}}}}
        """.trimIndent()
        val task = json.decodeFromString(PcCareTaskDto.serializer(), body)
        assertEquals(listOf("mixing_video", "spraying_video"), task.expectedSlots.map { it.fieldKey })
        val card = task.sop!!.categories.getValue("fumigation")
        assertEquals(listOf("mixing_video", "spraying_video", "sprayed_pen_photo"), card.proofs.map { it.key })
        assertEquals("photo", card.proofs[2].kind)
        assertEquals(10, task.sop!!.categories.getValue("hoof_trimming").proofs[0].minSeconds)
    }
}
