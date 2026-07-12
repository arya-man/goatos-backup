package sg.mesha.goatos.core.data.sync

/**
 * Cancels all outbox sync work: the periodic drain job, any pending backoff-retry one-time
 * work, and the persisted retry-schedule marker (logout clean-slate, C35-001) — so nothing
 * tries to drain an outbox that was just wiped, or resurrect a retry aimed at the departing
 * user's queued writes.
 *
 * Kept as its OWN SAM port rather than folded into [SyncRetryScheduler] — that one is already
 * a `fun interface` consumed via `SyncRetryScheduler { }` lambda syntax (see
 * [SyncEngine.Companion.Noop]); adding a second abstract method there would break that
 * single-abstract-method contract at every existing call site. Implemented by the
 * WorkManager-backed `sg.mesha.goatos.sync.SyncWorkScheduler` in `:app`; core-data stays
 * framework-free (mirrors [SyncRetryScheduler]'s own doc note).
 */
fun interface SyncJobsCanceller {
    fun cancelAll()

    companion object {
        val Noop = SyncJobsCanceller { }
    }
}

/** Wipes the entire outbox (logout clean-slate, C35-001) — see [sg.mesha.goatos.core.database.outbox.OutboxDao.clearAll].
 *  A dedicated SAM port (not folded into the full [OutboxStore]) so callers that only need
 *  "wipe everything" (namely `LogoutCoordinator`) can be unit-tested with a one-line fake
 *  instead of a full [OutboxStore] double. */
fun interface OutboxWiper {
    suspend fun clearAll()
}

/**
 * Symmetric counterpart to [SyncJobsCanceller]: (re-)arms the periodic outbox-drain
 * WorkManager job for the just-established session. [LogoutCoordinator]'s wipe cancels that
 * job, so a fresh sign-in in the SAME app process (no process restart, so
 * `GoatOsApplication.onCreate`'s one-time `schedule()` call never re-runs) must explicitly
 * re-arm it — otherwise the new session would silently lose the process-death-surviving sync
 * backstop for the rest of the app's lifetime. Kept as its own SAM port (not the concrete
 * WorkManager-backed `sg.mesha.goatos.sync.SyncWorkScheduler`) so `SessionViewModel` stays
 * unit-testable with a fake, without needing a real Android `Context`/Robolectric.
 */
fun interface SyncJobsScheduler {
    fun scheduleAll()
}
