package sg.mesha.goatos.capture

import kotlinx.coroutines.channels.Channel
import java.util.concurrent.atomic.AtomicLong

/**
 * Carries one recorder result back to the ONE capture request it was actually shot for.
 *
 * This exists because the recorder and the requester are decoupled in time. CameraX finalization
 * is asynchronous: the operator presses stop, walks to the next animal and scans it, and only
 * THEN does the first clip finalize. Meanwhile a scan of a different animal cancels the first
 * request and opens a new one (see `ScanViewModel.requestGoatProof` /
 * `WeighingViewModel.captureVideoForRow`). With a single shared result channel and an untagged
 * result, the first animal's finished clip is simply handed to whoever is receiving next — the
 * SECOND animal — and is persisted as that animal's medical proof. Nothing downstream can detect
 * the swap.
 *
 * The fix is identity, not timing: every request takes a [nextRequestToken], the recorder is
 * composed FOR that token and reports it back with its result, and [awaitResult] only ever
 * returns the result carrying its own token. A result belonging to a cancelled or superseded
 * request is discarded through [onDiscardedResult] (which deletes the orphan file) — never
 * re-attributed, and never left in the buffer to poison the next request.
 *
 * The channel is UNLIMITED on purpose. A rendezvous channel would make `trySend` DROP a result
 * whenever no one is waiting, which is fail-safe rather than fail-wrong but silently throws away
 * a real recording and still hands a result to a waiting-but-wrong receiver; buffering plus token
 * matching keeps every result accounted for and correctly attributed.
 */
class ProofCaptureRelay(
    private val onDiscardedResult: (CapturedVideo) -> Unit = {},
) {
    private data class Envelope(val token: Long, val video: CapturedVideo?)

    private val results = Channel<Envelope>(capacity = Channel.UNLIMITED)
    private val tokens = AtomicLong(0L)

    /** Claims a fresh identity for one capture request. */
    fun nextRequestToken(): Long = tokens.incrementAndGet()

    /**
     * Called when a recording finalizes. [token] is the request the recorder was composed for —
     * captured at composition time, never read at delivery time, so a recorder can never report
     * under a request that replaced it.
     */
    fun deliverResult(token: Long, video: CapturedVideo?) {
        results.trySend(Envelope(token, video))
    }

    /** Suspends until [token]'s OWN result arrives. Results for other (cancelled or superseded)
     *  requests are discarded here, so they can neither be returned to this request nor survive
     *  in the buffer for the next one. */
    suspend fun awaitResult(token: Long): CapturedVideo? {
        while (true) {
            val envelope = results.receive()
            if (envelope.token == token) {
                return envelope.video
            }
            envelope.video?.let(onDiscardedResult)
        }
    }
}
