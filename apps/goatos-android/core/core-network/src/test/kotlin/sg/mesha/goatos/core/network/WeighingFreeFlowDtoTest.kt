package sg.mesha.goatos.core.network

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WeighingAcceptedObservationDto
import sg.mesha.goatos.core.network.dto.WeighingObservationDto
import sg.mesha.goatos.core.network.dto.WeighingRosterResponseDto

/**
 * Weighing is FREE-FLOW: the backend dropped `weighing_observations.animal_id` (migration 000078)
 * and `weighing_expected_animals` (000079). A capture is a scanned tag and a weight and never
 * resolves to herd identity, so `scanned_identifier` is the only identity these payloads carry.
 *
 * These decode against payloads shaped exactly as the live handler emits them — no `animal_id` key
 * at all, and `items: []` on the scope read.
 */
class WeighingFreeFlowDtoTest {

    private val json = Json { ignoreUnknownKeys = true }

    // THE DTO DEFECT: `animalId` used to be a non-nullable `String = ""` on the accepted-observation
    // DTO, and the sync did `scannedIdentifier.ifBlank { animalId }`. With the backend no longer
    // sending the field the fallback resolved to "" and ERASED the scanned identifier.
    @Test
    fun `accepted observation with no animal_id keeps the scanned identifier`() {
        val payload = """
            {
              "observation_id": "obs-1",
              "campaign_id": "campaign-1",
              "campaign_shed_id": "shed-1",
              "scanned_identifier": "WG-RFID-0009",
              "weight_kg": 12.4,
              "proof_artifact_id": "proof-1",
              "expected_location_id": "loc-1",
              "accepted_at": "2026-07-30T10:00:00Z"
            }
        """.trimIndent()

        val decoded = json.decodeFromString<WeighingAcceptedObservationDto>(payload)

        assertEquals("WG-RFID-0009", decoded.scannedIdentifier)
        assertEquals("obs-1", decoded.observationId)
        assertEquals(12.4, decoded.weightKg, 0.0)
    }

    // A stale server that still emits animal_id must not resurrect it as an identity: the field is
    // simply not bound, and the scanned tag is unaffected.
    @Test
    fun `a legacy animal_id key is ignored and does not shadow the scanned identifier`() {
        val payload = """
            {
              "observation_id": "obs-2",
              "animal_id": "legacy-animal-2",
              "scanned_identifier": "WG-RFID-0010",
              "weight_kg": 13.5,
              "proof_artifact_id": "proof-2",
              "accepted_at": "2026-07-30T10:05:00Z"
            }
        """.trimIndent()

        assertEquals(
            "WG-RFID-0010",
            json.decodeFromString<WeighingAcceptedObservationDto>(payload).scannedIdentifier,
        )
    }

    @Test
    fun `leadership observation with no animal_id keeps the scanned identifier`() {
        val payload = """
            {
              "observation_id": "obs-3",
              "campaign_id": "campaign-1",
              "scanned_identifier": "WG-RFID-0011",
              "weight_kg": 18.25,
              "accepted_at": "2026-07-29T06:00:00Z"
            }
        """.trimIndent()

        val decoded = json.decodeFromString<WeighingObservationDto>(payload)

        assertEquals("WG-RFID-0011", decoded.scannedIdentifier)
        assertEquals(18.25, decoded.weightKg, 0.0)
    }

    // The scope read's `items` array is permanently empty (weighing_expected_animals is gone), so
    // the DTO deliberately does not bind it. Decoding must still succeed and surface the
    // observations, which are the whole payload now.
    @Test
    fun `scope read decodes with an empty items array it no longer binds`() {
        val payload = """
            {
              "items": [],
              "observations": [
                {
                  "observation_id": "obs-4",
                  "scanned_identifier": "WG-RFID-0012",
                  "weight_kg": 11.0,
                  "proof_artifact_id": "proof-4",
                  "accepted_at": "2026-07-30T11:00:00Z"
                }
              ],
              "next_cursor": "",
              "next_observations_cursor": null,
              "trace_id": "trace-1"
            }
        """.trimIndent()

        val decoded = json.decodeFromString<WeighingRosterResponseDto>(payload)

        assertEquals(listOf("WG-RFID-0012"), decoded.observations.map { it.scannedIdentifier })
        assertTrue(decoded.nextObservationsCursor == null)
    }
}
