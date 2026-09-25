package sg.mesha.goatos.leadershiptasks

import java.io.File

/** The one app-private folder a task draft's voice notes and picked files are copied into. */
const val LEADERSHIP_TASK_DRAFT_DIR = "leadership-task-drafts"

/**
 * Housekeeping for [LEADERSHIP_TASK_DRAFT_DIR].
 *
 * A voice note or a picked file is copied here so its bytes outlive the recorder / picker grant
 * until the task is sent. Nothing ever removed them, so every note a director recorded stayed on
 * the phone forever. A draft file is now deleted when its upload is done with it (the task was
 * sent), when it is removed from the draft, and when the form is left without sending; and each
 * time the form opens, anything older than [STALE_AFTER_MS] (a crash or a process death between
 * copy and send) is swept, bounded to [MAX_SWEEP] files per pass.
 *
 * Every delete is confined to the draft folder: a path outside it (an attachment the server
 * already held has no local path at all) is never touched.
 */
object LeadershipDraftFiles {
    /** A draft file left behind for a week is an orphan: no screen still holds it. */
    const val STALE_AFTER_MS: Long = 7L * 24L * 60L * 60L * 1000L

    /** One sweep never walks an unbounded folder. */
    const val MAX_SWEEP: Int = 200

    fun draftDir(filesDir: File): File = File(filesDir, LEADERSHIP_TASK_DRAFT_DIR)

    /** Deletes [path] when it is a file inside [draftDir]; returns true when a file was removed. */
    fun delete(path: String, draftDir: File): Boolean {
        if (path.isBlank()) return false
        val file = File(path)
        val root = draftDir.canonicalFile
        val parent = file.canonicalFile.parentFile ?: return false
        if (parent != root || !file.isFile) return false
        return file.delete()
    }

    /** Deletes draft files last modified more than [maxAgeMs] before [nowMs]; returns how many. */
    fun pruneStale(draftDir: File, nowMs: Long, maxAgeMs: Long = STALE_AFTER_MS): Int {
        val files = draftDir.listFiles() ?: return 0
        var removed = 0
        files.asSequence()
            .filter { it.isFile && nowMs - it.lastModified() > maxAgeMs }
            .take(MAX_SWEEP)
            .forEach { if (it.delete()) removed += 1 }
        return removed
    }
}
