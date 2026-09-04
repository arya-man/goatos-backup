package sg.mesha.goatos.leadershiptasks

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import android.os.SystemClock
import java.io.File

/** The finished voice note: where its bytes are and how long it runs. */
data class RecordedVoiceNote(
    val localPath: String,
    val durationMs: Long,
    val sizeBytes: Long,
)

/**
 * One in-app voice recording at a time. Production wraps [MediaRecorder]; tests substitute a
 * fake so the ViewModel's recording state machine is provable without a microphone.
 */
interface VoiceNoteRecorder {
    /** Starts recording into a fresh app-private file. Throws when the recorder cannot start. */
    fun start()

    /** Stops and returns the finished note, or null when nothing was recording. */
    fun stop(): RecordedVoiceNote?

    /** Drops an in-flight recording and its file. Safe when nothing is recording. */
    fun discard()
}

/**
 * MediaRecorder-backed voice notes: AAC in an MPEG-4 container (`audio/mp4`, `.m4a`) under
 * `filesDir/leadership-task-drafts`, the same private tree the attachment importer writes to.
 */
class MediaRecorderVoiceNoteRecorder(
    private val context: Context,
) : VoiceNoteRecorder {
    private var recorder: MediaRecorder? = null
    private var file: File? = null
    private var startedAtMs: Long = 0L

    override fun start() {
        discard()
        val dir = File(context.filesDir, DRAFT_DIR).apply { mkdirs() }
        val target = File(dir, "voice-${System.currentTimeMillis()}.m4a")
        @Suppress("DEPRECATION")
        val created = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) MediaRecorder(context) else MediaRecorder()
        try {
            created.setAudioSource(MediaRecorder.AudioSource.MIC)
            created.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            created.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            created.setAudioEncodingBitRate(AUDIO_BIT_RATE)
            created.setAudioSamplingRate(AUDIO_SAMPLE_RATE)
            created.setOutputFile(target.absolutePath)
            created.prepare()
            created.start()
        } catch (error: Exception) {
            runCatching { created.release() }
            target.delete()
            throw error
        }
        recorder = created
        file = target
        startedAtMs = SystemClock.elapsedRealtime()
    }

    override fun stop(): RecordedVoiceNote? {
        val current = recorder ?: return null
        val target = file
        val durationMs = (SystemClock.elapsedRealtime() - startedAtMs).coerceAtLeast(0L)
        // exception:exempt a stop() on a recorder that captured nothing throws; the note is then
        // simply not kept, which is the honest outcome for an empty recording
        val stopped = runCatching { current.stop() }.isSuccess
        runCatching { current.release() }
        recorder = null
        file = null
        if (!stopped || target == null || !target.isFile || target.length() == 0L) {
            target?.delete()
            return null
        }
        return RecordedVoiceNote(localPath = target.absolutePath, durationMs = durationMs, sizeBytes = target.length())
    }

    override fun discard() {
        val current = recorder ?: return
        runCatching { current.stop() }
        runCatching { current.release() }
        file?.delete()
        recorder = null
        file = null
    }

    private companion object {
        const val DRAFT_DIR = "leadership-task-drafts"
        const val AUDIO_BIT_RATE = 96_000
        const val AUDIO_SAMPLE_RATE = 44_100
    }
}
