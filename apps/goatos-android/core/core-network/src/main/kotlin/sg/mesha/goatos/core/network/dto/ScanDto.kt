package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class ScanRosterRowDto(
    @SerialName("goatId") val goatId: String = "",
    @SerialName("primaryTag") val primaryTag: String = "",
    @SerialName("secondaryTag") val secondaryTag: String? = null,
    @SerialName("vaccineLabel") val vaccineLabel: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("scannedAt") val scannedAt: String? = null,
    @SerialName("obligationId") val obligationId: String = "",
)

@Serializable
data class ScanRosterResponseDto(
    @SerialName("source") val source: String = "",
    @SerialName("taskId") val taskId: String = "",
    @SerialName("sopVersionId") val sopVersionId: String = "",
    @SerialName("taskRowVersion") val taskRowVersion: Int = 0,
    @SerialName("batchId") val batchId: String = "",
    @SerialName("rows") val rows: List<ScanRosterRowDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class RescheduleObligationRequestDto(
    @SerialName("due_at") val dueAt: String,
    @SerialName("window_start") val windowStart: String? = null,
    @SerialName("window_end") val windowEnd: String? = null,
)

@Serializable
data class RescheduleObligationResponseDto(
    @SerialName("obligation_id") val obligationId: String = "",
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)
