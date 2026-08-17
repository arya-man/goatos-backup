package sg.mesha.goatos.core.proofedit

import org.junit.Test
import org.junit.Assert.assertEquals

class ProofClipTest {

    @Test
    fun `kept ranges stay in source order and are never resequenced`() {
        val clips = listOf(
            ProofClip(45_000, 50_000),
            ProofClip(1_000, 10_000),
            ProofClip(20_000, 30_000),
        )
        assertEquals(
            listOf(ProofClip(1_000, 10_000), ProofClip(20_000, 30_000), ProofClip(45_000, 50_000)),
            clips.normalized(),
        )
    }

    @Test
    fun `overlapping keeps merge so footage is never duplicated`() {
        val clips = listOf(ProofClip(0, 10_000), ProofClip(5_000, 15_000))
        assertEquals(listOf(ProofClip(0, 15_000)), clips.normalized())
        assertEquals(15_000L, clips.normalized().totalDurationMs())
    }

    @Test
    fun `touching keeps merge into one contiguous range`() {
        val clips = listOf(ProofClip(0, 10_000), ProofClip(10_000, 20_000))
        assertEquals(listOf(ProofClip(0, 20_000)), clips.normalized())
    }

    @Test
    fun `output duration is the sum of kept ranges only`() {
        val clips = listOf(ProofClip(1_000, 10_000), ProofClip(20_000, 30_000), ProofClip(45_000, 50_000))
        assertEquals(24_000L, clips.totalDurationMs())
    }

    @Test
    fun `kept share reports full when nothing was cut`() {
        assertEquals("full", trimKeptShareBucket(keptMs = 60_000, sourceMs = 60_000))
    }

    @Test
    fun `kept share buckets are coarse and non identifying`() {
        assertEquals("0_24", trimKeptShareBucket(10_000, 60_000))
        assertEquals("25_49", trimKeptShareBucket(20_000, 60_000))
        assertEquals("50_74", trimKeptShareBucket(35_000, 60_000))
        assertEquals("75_99", trimKeptShareBucket(50_000, 60_000))
        assertEquals("unknown", trimKeptShareBucket(10_000, 0))
    }

    @Test
    fun `clip count buckets stay low cardinality`() {
        assertEquals("none", trimClipCountBucket(0))
        assertEquals("1", trimClipCountBucket(1))
        assertEquals("2_3", trimClipCountBucket(3))
        assertEquals("4_6", trimClipCountBucket(5))
        assertEquals("7_plus", trimClipCountBucket(40))
    }
}
