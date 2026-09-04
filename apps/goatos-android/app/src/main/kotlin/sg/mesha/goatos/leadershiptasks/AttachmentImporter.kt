package sg.mesha.goatos.leadershiptasks

import android.content.Context
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.provider.OpenableColumns
import android.webkit.MimeTypeMap
import java.io.File
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** A picked file copied into app-private storage, with what the content resolver knew about it. */
data class ImportedAttachment(
    val localPath: String,
    val fileName: String,
    val mimeType: String,
    val sizeBytes: Long,
    /** Media duration when the file is audio/video and it could be read; null otherwise. */
    val durationMs: Long?,
)

/**
 * Copies a picker's `content://` result into this app's private storage so the bytes outlive the
 * picker's transient grant and can be uploaded later. Tests substitute a fake.
 */
interface AttachmentImporter {
    /**
     * Copies [uri] into app-private storage. [maxBytes] refuses a file larger than the cap BEFORE
     * copying, returning [ImportResult.TooLarge]; any other failure is [ImportResult.Failed].
     */
    suspend fun import(uri: String, maxBytes: Long): ImportResult
}

sealed interface ImportResult {
    data class Imported(val attachment: ImportedAttachment) : ImportResult
    data class TooLarge(val sizeBytes: Long) : ImportResult
    data class Failed(val cause: Throwable) : ImportResult
}

/** Production importer over [android.content.ContentResolver], always off the main thread. */
class ContentResolverAttachmentImporter(
    private val context: Context,
) : AttachmentImporter {
    override suspend fun import(uri: String, maxBytes: Long): ImportResult = withContext(Dispatchers.IO) {
        try {
            val parsed = Uri.parse(uri)
            val resolver = context.contentResolver
            var displayName = ""
            var declaredSize = -1L
            resolver.query(parsed, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameIndex >= 0) displayName = cursor.getString(nameIndex).orEmpty()
                    if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) declaredSize = cursor.getLong(sizeIndex)
                }
            }
            if (declaredSize > maxBytes) return@withContext ImportResult.TooLarge(declaredSize)
            val mimeType = resolver.getType(parsed)?.takeIf { it.isNotBlank() }
                ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(displayName.substringAfterLast('.', "").lowercase())
                ?: DEFAULT_MIME
            val extension = displayName.substringAfterLast('.', "").takeIf { it.isNotBlank() && it.length <= MAX_EXT_CHARS }
                ?: MimeTypeMap.getSingleton().getExtensionFromMimeType(mimeType).orEmpty()
            val dir = File(context.filesDir, DRAFT_DIR).apply { mkdirs() }
            val target = File(dir, "pick-${System.currentTimeMillis()}-${(0..999_999).random()}" + if (extension.isBlank()) "" else ".$extension")
            val input = resolver.openInputStream(parsed) ?: throw IOException("cannot open $uri")
            var copied = 0L
            input.use { source ->
                target.outputStream().use { sink ->
                    val buffer = ByteArray(COPY_BUFFER_BYTES)
                    while (true) {
                        val read = source.read(buffer)
                        if (read < 0) break
                        copied += read
                        if (copied > maxBytes) {
                            sink.close()
                            target.delete()
                            return@withContext ImportResult.TooLarge(copied)
                        }
                        sink.write(buffer, 0, read)
                    }
                }
            }
            val fileName = displayName.ifBlank { target.name }
            val durationMs = if (mimeType.startsWith("audio/") || mimeType.startsWith("video/")) readDurationMs(target) else null
            ImportResult.Imported(
                ImportedAttachment(
                    localPath = target.absolutePath,
                    fileName = fileName,
                    mimeType = mimeType,
                    sizeBytes = copied,
                    durationMs = durationMs,
                ),
            )
        } catch (error: Exception) {
            ImportResult.Failed(error)
        }
    }

    private fun readDurationMs(file: File): Long? {
        val retriever = MediaMetadataRetriever()
        // exception:exempt a media file whose duration cannot be read still attaches; the label
        // is simply absent, and the server reads the real duration on completion
        return try {
            retriever.setDataSource(file.absolutePath)
            retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull()
        } catch (_: Exception) {
            null
        } finally {
            runCatching { retriever.release() }
        }
    }

    private companion object {
        const val DRAFT_DIR = "leadership-task-drafts"
        const val DEFAULT_MIME = "application/octet-stream"
        const val MAX_EXT_CHARS = 8
        const val COPY_BUFFER_BYTES = 64 * 1024
    }
}
