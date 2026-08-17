package sg.mesha.goatos.core.proofedit

/**
 * One keep-range in the SOURCE clip's timeline. The operator marks several (for example
 * 1s-10s, 20s-30s, 45s-50s) and the export concatenates them in SOURCE order.
 *
 * There is deliberately no reordering: a proof video's clips must stay in the order the work
 * actually happened, so a verifier reading the clip cannot be shown a resequenced account of it.
 */
data class ProofClip(val startMs: Long, val endMs: Long) {
    val durationMs: Long get() = (endMs - startMs).coerceAtLeast(0L)
}

fun List<ProofClip>.totalDurationMs(): Long = sumOf { it.durationMs }

/**
 * Chronological and non-overlapping. Overlapping keeps would duplicate footage in the output,
 * which would misrepresent how long the work took.
 */
fun List<ProofClip>.normalized(): List<ProofClip> {
    val sorted = sortedBy { it.startMs }
    val out = mutableListOf<ProofClip>()
    sorted.forEach { clip ->
        val last = out.lastOrNull()
        if (last != null && clip.startMs <= last.endMs) {
            out[out.lastIndex] = last.copy(endMs = maxOf(last.endMs, clip.endMs))
        } else {
            out.add(clip)
        }
    }
    return out
}

/** Coarse, non-PII buckets so trim telemetry stays inside the Firebase param budget. */
fun trimClipCountBucket(count: Int): String = when {
    count <= 0 -> "none"
    count == 1 -> "1"
    count in 2..3 -> "2_3"
    count in 4..6 -> "4_6"
    else -> "7_plus"
}

/** Share of the source the operator KEPT, bucketed. `full` means nothing was cut. */
fun trimKeptShareBucket(keptMs: Long, sourceMs: Long): String {
    if (sourceMs <= 0L) return "unknown"
    if (keptMs >= sourceMs) return "full"
    return when ((keptMs * 100L) / sourceMs) {
        in 0..24 -> "0_24"
        in 25..49 -> "25_49"
        in 50..74 -> "50_74"
        else -> "75_99"
    }
}
