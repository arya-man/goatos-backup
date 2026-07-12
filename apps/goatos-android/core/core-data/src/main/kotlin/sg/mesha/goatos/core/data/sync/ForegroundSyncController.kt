package sg.mesha.goatos.core.data.sync

/**
 * Port [DefaultSyncRepository] (and app startup) use to ask for the Drive/Photos-style
 * **background upload foreground service** (MOB-002,
 * `docs/mobile/proof-capture-sync-and-e2e.md` §3 "Background sync survives app close") to be
 * running. The concrete Android implementation
 * (`sg.mesha.goatos.sync.AndroidForegroundSyncController`, `:app`) starts
 * `sg.mesha.goatos.sync.UploadForegroundService`, which shows an ongoing progress notification
 * and drives [SyncEngine.drainOnce] via [UploadSyncCoordinator] until the upload-relevant
 * outbox rows are exhausted or the pass stalls (offline/backed off).
 *
 * Kept as a port (not a direct `:app` dependency) so `:core:core-data` — where the real
 * enqueue/drain logic lives — never depends on `android.app.Service` or Hilt's
 * `@AndroidEntryPoint`, exactly like [ConnectivityGate] and [SyncRetryScheduler] already do for
 * their platform pieces.
 */
fun interface ForegroundSyncController {
    /**
     * Idempotent: ensure the background upload service is running. Cheap and safe to call on
     * every relevant enqueue AND once at app startup (the resume-after-process-restart /
     * resume-after-kill path) — if there is nothing upload-relevant queued, the service notices
     * on its first pass and stops itself immediately (see [UploadSyncCoordinator.Outcome.Idle]).
     */
    fun ensureRunning()

    companion object {
        /** Test/DI default — used wherever no real foreground-service wiring is available
         *  (unit tests, and any [DefaultSyncRepository] construction that predates this port). */
        val Noop = ForegroundSyncController { }
    }
}
