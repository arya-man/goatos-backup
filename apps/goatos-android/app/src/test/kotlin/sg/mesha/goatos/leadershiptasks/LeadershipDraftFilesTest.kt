package sg.mesha.goatos.leadershiptasks

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * Voice notes and picked files were copied into `filesDir/leadership-task-drafts` and never
 * removed. These pin the housekeeping: a draft file is deleted on demand, only inside the draft
 * folder, and a bounded sweep removes what a crash left behind for over a week.
 */
class LeadershipDraftFilesTest {
    @get:Rule
    val temp = TemporaryFolder()

    private fun draftDir(): File = LeadershipDraftFiles.draftDir(temp.root).apply { mkdirs() }

    @Test
    fun `a draft file is deleted, a path outside the draft folder never is`() {
        val dir = draftDir()
        val note = File(dir, "voice-1.m4a").apply { writeText("aac") }
        val outside = temp.newFile("keep.jpg")

        assertTrue(LeadershipDraftFiles.delete(note.absolutePath, dir))
        assertFalse(note.exists())
        assertFalse("never outside the draft folder", LeadershipDraftFiles.delete(outside.absolutePath, dir))
        assertTrue(outside.exists())
        assertFalse("an attachment the server already holds has no local path", LeadershipDraftFiles.delete("", dir))
        assertFalse(LeadershipDraftFiles.delete(File(dir, "../keep.jpg").path, dir))
        assertTrue(outside.exists())
    }

    @Test
    fun `the sweep removes only files older than a week`() {
        val dir = draftDir()
        val now = 1_000L * 24L * 60L * 60L * 1000L
        val stale = File(dir, "pick-old.pdf").apply { writeText("x"); setLastModified(now - LeadershipDraftFiles.STALE_AFTER_MS - 1) }
        val fresh = File(dir, "voice-new.m4a").apply { writeText("y"); setLastModified(now - 60_000L) }

        assertEquals(1, LeadershipDraftFiles.pruneStale(dir, now))
        assertFalse(stale.exists())
        assertTrue(fresh.exists())
    }

    @Test
    fun `the sweep is bounded and tolerates a missing folder`() {
        assertEquals(0, LeadershipDraftFiles.pruneStale(File(temp.root, "absent"), 10L))
        val dir = draftDir()
        repeat(LeadershipDraftFiles.MAX_SWEEP + 5) { i -> File(dir, "f$i").apply { writeText("z"); setLastModified(0L) } }
        assertEquals(LeadershipDraftFiles.MAX_SWEEP, LeadershipDraftFiles.pruneStale(dir, LeadershipDraftFiles.STALE_AFTER_MS * 2))
        assertEquals(5, dir.listFiles()!!.size)
    }
}
