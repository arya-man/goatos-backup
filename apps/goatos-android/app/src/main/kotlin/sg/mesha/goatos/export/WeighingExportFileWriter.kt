package sg.mesha.goatos.export

import android.content.Context
import android.net.Uri
import androidx.core.content.FileProvider
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.File
import javax.inject.Inject

/**
 * Writes a downloaded weighing CSV export to disk and hands back a shareable content [Uri].
 *
 * A PORT, same reasoning as [sg.mesha.goatos.rfid.RfidReaderPort]: the ViewModel that requests an
 * export stays free of [Context] and [FileProvider], which keeps it constructible in tests without
 * an Android runtime. It throws on a write failure rather than swallowing it -- the caller is
 * expected to report the exception, matching the exception-guard rule this repo enforces
 * everywhere else (see WeighingViewModel's crashReporter.recordException usage).
 */
interface WeighingExportFileWriter {
    /** Writes [bytes] under [fileName] and returns a `content://` [Uri] a share sheet can open. */
    fun write(bytes: ByteArray, fileName: String): Uri
}

/**
 * Cache-backed implementation. Exports are re-downloaded on every request (see
 * [sg.mesha.goatos.core.data.weighing.WeighingRepository.exportCampaignCsv]), so the file itself
 * is disposable — `cacheDir` is the correct home, and the OS is free to reclaim it under storage
 * pressure without losing anything the app cannot fetch again.
 */
class AndroidWeighingExportFileWriter @Inject constructor(
    @ApplicationContext private val context: Context,
    private val crashReporter: sg.mesha.goatos.core.analytics.CrashReporter,
) : WeighingExportFileWriter {
    override fun write(bytes: ByteArray, fileName: String): Uri {
        val exportsDir = File(context.cacheDir, "exports").apply { mkdirs() }
        // Prune EARLIER exports before writing this one. Each export is a full copy of a task's
        // weighing data; without this they accumulate for the life of the install. The directory
        // is app-private so this is housekeeping, not a leak fix -- but a phone that fills up
        // stops being able to record work, and farm data has no business lingering on a handset
        // longer than the share it was created for.
        //
        // Best-effort by design: a file the OS still has open must not fail the export the
        // operator asked for.
        runCatching {
            exportsDir.listFiles()?.forEach { stale -> stale.delete() }
        }.onFailure { error ->
            crashReporter.recordException(error, "AndroidWeighingExportFileWriter.prune")
        }
        val file = File(exportsDir, fileName)
        file.writeBytes(bytes)
        return FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
    }
}
