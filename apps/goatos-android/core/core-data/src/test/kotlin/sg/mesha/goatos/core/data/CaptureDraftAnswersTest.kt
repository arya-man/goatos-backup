package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CaptureEvidenceDraftEntity

/**
 * Regression for the maintainer report of 2026-07-31: on Milk Preparation and Milk Feeding, an
 * operator recorded videos, entered the quantities/remarks beside them, pressed Back, and returned
 * to find the videos intact and every typed field blank.
 *
 * The videos survived because they were already written to `capture_evidence_drafts`. The answers
 * did not, because they lived in `SavedStateHandle`, which dies with the nav backstack entry. These
 * tests pin the durable half of the fix at the storage layer: what a screen writes before leaving is
 * what a FRESH read — the shape a rebuilt ViewModel performs on re-entry — gets back.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CaptureDraftAnswersTest {

    private fun withRepository(block: suspend (CaptureDraftRepository, GoatDatabase) -> Unit) = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            block(DefaultCaptureDraftRepository(db) { 1_000L }, db)
        } finally {
            db.close()
        }
    }

    @Test
    fun `typed answers survive the screen being rebuilt`() = withRepository { drafts, _ ->
        val entityId = "park-1:2026-07-31"
        drafts.putAnswers(
            CaptureFlow.MILK_PREPARATION,
            entityId,
            mapOf("morning" to "12.5", "evening" to "9", "step:boiling_temperature" to "72"),
        )

        // The read a rebuilt ViewModel performs in init. Nothing is carried over in memory.
        val restored = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)

        assertEquals("12.5", restored.answer("morning"))
        assertEquals("9", restored.answer("evening"))
        assertEquals("72", restored.answer("step:boiling_temperature"))
    }

    @Test
    fun `answers and proofs are independent halves of the same work item`() = withRepository { drafts, _ ->
        val entityId = "task-9"
        drafts.putProof(CaptureFlow.MILK_FEEDING, entityId, "clean_bottles", "outbox-1")
        drafts.putAnswers(CaptureFlow.MILK_FEEDING, entityId, mapOf("total" to "40"))

        val restored = drafts.find(CaptureFlow.MILK_FEEDING, entityId)

        // Writing answers must not disturb a recorded clip, and vice versa — this is the exact
        // pairing the bug broke, where one came back and the other did not.
        assertTrue(restored.hasProof("clean_bottles"))
        assertEquals("outbox-1", restored.proofs["clean_bottles"])
        assertEquals("40", restored.answer("total"))
    }

    @Test
    fun `re-saving replaces the whole answer set so a cleared field stays cleared`() = withRepository { drafts, _ ->
        val entityId = "park-2:2026-07-31"
        drafts.putAnswers(CaptureFlow.MILK_PREPARATION, entityId, mapOf("morning" to "12.5", "evening" to "9"))
        drafts.putAnswers(CaptureFlow.MILK_PREPARATION, entityId, mapOf("morning" to "12.5", "evening" to ""))

        val restored = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)

        // A field the operator deliberately emptied must not resurrect from the previous save.
        assertEquals("12.5", restored.answer("morning"))
        assertEquals("", restored.answer("evening"))
    }

    @Test
    fun `the answers row is not counted as a captured proof`() = withRepository { drafts, _ ->
        val entityId = "task-10"
        drafts.putAnswers(CaptureFlow.MILK_FEEDING, entityId, mapOf("total" to "40"))
        drafts.putSubmit(CaptureFlow.MILK_FEEDING, entityId, "milk-feeding-submit:$entityId", null)

        // The list's progress decoration counts VIDEOS. Neither reserved row is one, so a task with
        // answers typed but nothing recorded must not read as "1 video saved".
        assertEquals(emptyMap<String, Int>(), drafts.observeProgress(CaptureFlow.MILK_FEEDING).first())

        drafts.putProof(CaptureFlow.MILK_FEEDING, entityId, "clean_bottles", "outbox-1")
        assertEquals(mapOf(entityId to 1), drafts.observeProgress(CaptureFlow.MILK_FEEDING).first())
    }

    @Test
    fun `an unreadable answers blob degrades to no answers instead of crashing`() = withRepository { drafts, db ->
        val entityId = "park-3:2026-07-31"
        // A row written by an older build, hand-edited, or truncated. A draft is a convenience, so
        // it must never be able to take down the screen it is restoring.
        db.captureEvidenceDraftDao().upsert(
            CaptureEvidenceDraftEntity(
                flowKey = CaptureFlow.MILK_PREPARATION,
                entityId = entityId,
                step = CaptureEvidenceDraftEntity.ANSWERS_STEP,
                answers = "{not json",
                updatedAt = 1_000L,
            ),
        )

        val restored = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)

        assertEquals(emptyMap<String, String>(), restored.answers)
        assertEquals("", restored.answer("morning"))
    }
}
