package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureDto
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureMediaDto
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureRowDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import kotlinx.serialization.json.Json

/**
 * SHIFTING SOP (2026-09-16): the approvals queue carries the backend's shared CountsApprovalCapture
 * verbatim -- decoding the wire JSON the server writes and mapping it onto the row the screen renders.
 */
class ApprovalCaptureRowMappingTest {
    @Test
    fun `the capture json decodes into the shared dto shape`() {
        val wire = """{"approval_request_id":"ar1","request_type":"shifting","status":"pending","capture":{"version_label":"SOP v2","rows":[{"label":"Why move","value":"Overcrowded","group":"At raise"}],"media":[{"proof_id":"r1","label":"Pen photo","kind":"photo"}],"missing_note":"Not captured (older app): Arrival photo"}}"""
        val item = Json { ignoreUnknownKeys = true }.decodeFromString(CountsApprovalListItemDto.serializer(), wire)
        assertEquals(
            CountsApprovalCaptureDto(
                versionLabel = "SOP v2",
                rows = listOf(CountsApprovalCaptureRowDto("Why move", "Overcrowded", "At raise")),
                media = listOf(CountsApprovalCaptureMediaDto("r1", "Pen photo", "photo")),
                missingNote = "Not captured (older app): Arrival photo",
            ),
            item.capture,
        )
    }
}
