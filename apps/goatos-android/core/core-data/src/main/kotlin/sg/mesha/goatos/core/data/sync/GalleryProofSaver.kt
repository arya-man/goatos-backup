package sg.mesha.goatos.core.data.sync

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Environment
import android.provider.MediaStore
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import java.io.File
import java.net.URI
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull

/** Best-effort operator convenience copy. Upload remains app-private/Room-first source of truth. */
fun interface GalleryProofSaver {
    suspend fun saveProofCopy(localFilePath: String, request: ProofUploadRequestDto, idempotencyKey: String)

    object Noop : GalleryProofSaver {
        override suspend fun saveProofCopy(localFilePath: String, request: ProofUploadRequestDto, idempotencyKey: String) = Unit
    }
}

class MediaStoreGalleryProofSaver(
    private val context: Context,
    private val nowMs: () -> Long = System::currentTimeMillis,
) : GalleryProofSaver {
    override suspend fun saveProofCopy(localFilePath: String, request: ProofUploadRequestDto, idempotencyKey: String) {
        if (localFilePath.isBlank()) return
        val mimeType = request.mimeType.ifBlank { fallbackMimeType(localFilePath, request.proofType) }
        // An audio note is not evidence the operator needs in their gallery; it stays app-private.
        if (mimeType.startsWith("audio/", ignoreCase = true) || request.proofType.equals("audio", ignoreCase = true)) return
        val isImage = mimeType.startsWith("image/", ignoreCase = true) || request.proofType.equals("photo", ignoreCase = true)
        val relativePath = galleryRelativePath(isImage)
        val displayName = proofDisplayName(request.proofType, idempotencyKey, mimeType, request.uploadOriginal)
        val collection = if (isImage) {
            MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
        } else {
            MediaStore.Video.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
        }
        if (existingProofCopy(collection, displayName, relativePath) != null) return
        val target = context.contentResolver.insert(
            collection,
            ContentValues().apply {
                put(MediaStore.MediaColumns.DISPLAY_NAME, displayName)
                put(MediaStore.MediaColumns.MIME_TYPE, mimeType)
                put(MediaStore.MediaColumns.RELATIVE_PATH, relativePath)
                put(MediaStore.MediaColumns.IS_PENDING, 1)
            },
        ) ?: return

        try {
            context.contentResolver.openOutputStream(target)?.use { output ->
                openInput(localFilePath)?.use { input -> input.copyTo(output) }
                    ?: error("proof source unavailable")
            } ?: error("gallery target unavailable")
            context.contentResolver.update(
                target,
                ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) },
                null,
                null,
            )
        } catch (error: Throwable) {
            runCatching { context.contentResolver.delete(target, null, null) }
            throw error
        }
    }

    private fun existingProofCopy(collection: Uri, displayName: String, relativePath: String): Uri? {
        val projection = arrayOf(MediaStore.MediaColumns._ID)
        val selection = "${MediaStore.MediaColumns.DISPLAY_NAME} = ? AND ${MediaStore.MediaColumns.RELATIVE_PATH} = ?"
        val args = arrayOf(displayName, "$relativePath/")
        context.contentResolver.query(collection, projection, selection, args, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                val id = cursor.getLong(cursor.getColumnIndexOrThrow(MediaStore.MediaColumns._ID))
                return Uri.withAppendedPath(collection, id.toString())
            }
        }
        return null
    }

    private fun openInput(localFilePath: String) =
        when {
            localFilePath.startsWith("content:", ignoreCase = true) ->
                context.contentResolver.openInputStream(Uri.parse(localFilePath))
            localFilePath.startsWith("file:", ignoreCase = true) ->
                File(URI(localFilePath)).inputStream()
            else -> File(localFilePath).inputStream()
        }
}

private fun galleryRelativePath(isImage: Boolean): String =
    if (isImage) {
        "${Environment.DIRECTORY_PICTURES}/GoatOS Proofs"
    } else {
        "${Environment.DIRECTORY_MOVIES}/GoatOS Proofs"
    }

private val ProofUploadRequestDto.uploadOriginal: Boolean
    get() = (metadata["upload_original"] as? JsonPrimitive)?.booleanOrNull == true

private fun proofDisplayName(proofType: String, idempotencyKey: String, mimeType: String, uploadOriginal: Boolean): String {
    val safeKey = idempotencyKey
        .takeLast(48)
        .replace(Regex("[^A-Za-z0-9._-]+"), "-")
        .trim('-')
        .ifBlank { "proof" }
    val safeType = proofType
        .lowercase()
        .replace(Regex("[^a-z0-9]+"), "-")
        .trim('-')
        .ifBlank { "proof" }
    val artifact = if (uploadOriginal) "original" else "processed"
    return "goatos_${safeType}_${artifact}_$safeKey.${extensionFor(mimeType)}"
}

private fun fallbackMimeType(localFilePath: String, proofType: String): String =
    when {
        proofType.equals("photo", ignoreCase = true) -> "image/jpeg"
        localFilePath.endsWith(".jpg", ignoreCase = true) || localFilePath.endsWith(".jpeg", ignoreCase = true) -> "image/jpeg"
        localFilePath.endsWith(".png", ignoreCase = true) -> "image/png"
        else -> "video/mp4"
    }

private fun extensionFor(mimeType: String): String =
    when (mimeType.lowercase()) {
        "image/jpeg", "image/jpg" -> "jpg"
        "image/png" -> "png"
        "image/webp" -> "webp"
        "video/quicktime" -> "mov"
        "video/webm" -> "webm"
        else -> "mp4"
    }
