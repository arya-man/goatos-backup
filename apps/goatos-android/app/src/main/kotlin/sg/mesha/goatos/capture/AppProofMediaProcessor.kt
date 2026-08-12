package sg.mesha.goatos.capture

import android.content.Context
import android.net.Uri
import dagger.hilt.android.qualifiers.ApplicationContext
import sg.mesha.goatos.core.data.capture.ProofMediaProcessingRequest
import sg.mesha.goatos.core.data.capture.ProofMediaProcessingResult
import sg.mesha.goatos.core.data.capture.ProofMediaProcessor
import java.io.File
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Production-safe proof media processor binding.
 *
 * A compressor/overlay engine can replace this class behind [ProofMediaProcessor] without changing
 * feature capture call sites. Until that engine lands, this processor selects the captured artifact
 * as the final upload artifact without throwing, so production captures are not all recorded as
 * failed processing.
 */
@Singleton
class AppProofMediaProcessor @Inject constructor(
    @ApplicationContext private val context: Context,
) : ProofMediaProcessor {
    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult {
        val bytes = fileBytes(request.originalUri) ?: contentBytes(request.originalUri)
        return ProofMediaProcessingResult(
            outputUri = request.originalUri,
            outputMimeType = request.mimeType,
            originalBytes = bytes,
            processedBytes = bytes,
        )
    }

    private fun fileBytes(uriText: String): Long? {
        val uri = Uri.parse(uriText)
        val path = when (uri.scheme) {
            null, "file" -> uri.path ?: uriText
            else -> return null
        }
        return File(path).takeIf { it.exists() }?.length()
    }

    private fun contentBytes(uriText: String): Long? =
        runCatching {
            context.contentResolver.openAssetFileDescriptor(Uri.parse(uriText), "r")
                ?.use { descriptor -> descriptor.length.takeIf { it >= 0L } }
        }.getOrNull()
}
