package sg.mesha.goatos.core.data.capture

data class ProofMediaProcessingRequest(
    val proofId: String,
    val taskId: String,
    val fieldKey: String,
    val subjectType: String,
    val subjectId: String?,
    val rfidTag: String?,
    val originalUri: String,
    val mimeType: String,
    val capturedStartMs: Long,
    val capturedEndMs: Long,
    val capturedByPrincipalId: String?,
    val locationAddress: String? = null,
    val latitude: Double? = null,
    val longitude: Double? = null,
    val gpsAccuracyM: Double? = null,
    val caption: String? = null,
)

data class ProofLocationSnapshot(
    val locationStatus: String,
    val latitude: Double? = null,
    val longitude: Double? = null,
    val gpsAccuracyM: Double? = null,
    val geocoderStatus: String? = null,
    val address: String? = null,
)

fun interface ProofLocationProvider {
    suspend fun snapshot(): ProofLocationSnapshot

    object Unavailable : ProofLocationProvider {
        override suspend fun snapshot(): ProofLocationSnapshot =
            ProofLocationSnapshot(locationStatus = "unavailable", geocoderStatus = "unavailable")
    }
}

data class ProofMediaProcessingResult(
    val outputUri: String,
    val outputMimeType: String,
    val originalBytes: Long?,
    val processedBytes: Long?,
    val inputWidth: Int? = null,
    val inputHeight: Int? = null,
    val targetVideoBitrate: Int? = null,
    val targetAudioBitrate: Int? = null,
)

fun interface ProofMediaProcessor {
    suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult

    object Noop : ProofMediaProcessor {
        override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult =
            throw UnsupportedOperationException("proof media processor is not wired")
    }
}
