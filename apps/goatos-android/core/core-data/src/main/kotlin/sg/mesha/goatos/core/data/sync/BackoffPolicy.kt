package sg.mesha.goatos.core.data.sync

/** How long [SyncEngine] waits before retrying attempt number [delayMillis]'s `attempt`
 *  (1-based: the value passed is the attempt that JUST failed, so `delayMillis(1)` is the
 *  wait before the 2nd try). */
fun interface BackoffPolicy {
    fun delayMillis(attempt: Int): Long

    companion object {
        /** Exponential backoff with a cap and jitter: base, 2x base, 4x base, ... capped at
         *  [capMillis], plus up to [jitterFraction] extra. Jitter is applied AFTER the cap (so
         *  the effective ceiling is `capMillis * (1 + jitterFraction)`, ~18 min for the
         *  defaults) — deliberately: the jitter must survive at the cap, otherwise every device
         *  that hit the ceiling would retry at the exact same instant (the thundering herd the
         *  jitter exists to prevent). */
        fun exponential(
            baseMillis: Long = 1_000L,
            capMillis: Long = 15 * 60 * 1_000L,
            jitterFraction: Double = 0.2,
            random: () -> Double = Math::random,
        ): BackoffPolicy = BackoffPolicy { attempt ->
            val shift = (attempt - 1).coerceIn(0, 20)
            val exponential = baseMillis * (1L shl shift)
            val capped = exponential.coerceAtMost(capMillis)
            val jitter = (capped * jitterFraction * random()).toLong()
            capped + jitter
        }

        /** 1s, 2s, 4s, 8s, ... capped at ~15 min, +0-20% jitter (so the true ceiling is ~18 min).
         *  Used by [SyncEngine] unless a test overrides it for determinism. */
        val Default: BackoffPolicy = exponential()
    }
}
