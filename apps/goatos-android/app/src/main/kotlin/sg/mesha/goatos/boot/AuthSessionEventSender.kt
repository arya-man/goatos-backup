package sg.mesha.goatos.boot

import android.util.Log
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import sg.mesha.goatos.core.network.BootstrapError
import sg.mesha.goatos.core.network.asBootstrapError
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.AuthSessionEventRequestDto
import java.util.concurrent.atomic.AtomicReference
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Sends the Goat OS `POST /auth/session-events` audit event OFF the login path.
 *
 * Firebase auth is the credential check. The session event is an audit record plus the
 * server-side claim of a pending email grant (`authaudit.claimPendingEmailGrant`); neither may
 * block or fail an operator's login. On 1.0.40 a brief DNS drop made this call throw
 * `UnknownHostException`, and because login awaited it, a correctly authenticated operator was
 * shown raw exception text and could not get in.
 *
 * So login opens the session first and hands the event here. It runs in the application scope
 * (survives the login screen leaving composition) and retries transient failures with backoff.
 * A 401/403 is a definitive answer from the server (e.g. `email_not_allowed`), not a transient
 * failure, so it is not retried; bootstrap will surface that principal's access state.
 *
 * Ordering with bootstrap: a first-ever login needs the pending-grant claim to land before
 * `/app/bootstrap` stops answering 403. [awaitDelivery] lets bootstrap wait (bounded) for the
 * in-flight event and retry once, instead of making every login wait for it up front.
 */
@Singleton
class AuthSessionEventSender(
    private val appApi: AppApi,
    private val scope: CoroutineScope,
    private val backoffMs: List<Long> = DEFAULT_BACKOFF_MS,
) {
    @Inject
    constructor(appApi: AppApi, scope: CoroutineScope) : this(appApi, scope, DEFAULT_BACKOFF_MS)

    private class Attempt(val job: Job, @Volatile var delivered: Boolean = false)

    private val current = AtomicReference<Attempt?>(null)

    /** Fire-and-forget; a newer sign-in supersedes any still-retrying older event. */
    fun send(eventType: String, source: String) {
        val request = AuthSessionEventRequestDto(eventType = eventType, source = source)
        lateinit var attempt: Attempt
        val job = scope.launch(start = kotlinx.coroutines.CoroutineStart.LAZY) {
            attempt.delivered = deliverWithRetry(request)
        }
        attempt = Attempt(job)
        current.getAndSet(attempt)?.job?.cancel()
        job.start()
    }

    /**
     * Waits up to [timeoutMs] for the most recent event to finish. Returns true when an event was
     * delivered (so a retry of anything that depended on it is worthwhile), false when there is
     * nothing in flight, it gave up, or the wait timed out.
     */
    suspend fun awaitDelivery(timeoutMs: Long): Boolean {
        val attempt = current.get() ?: return false
        withTimeoutOrNull(timeoutMs) { attempt.job.join() }
        return attempt.delivered
    }

    private suspend fun deliverWithRetry(request: AuthSessionEventRequestDto): Boolean {
        var attempt = 0
        while (true) {
            try {
                appApi.recordAuthSessionEvent(request)
                return true
            } catch (c: CancellationException) {
                throw c
            } catch (t: Throwable) {
                if (!isRetryable(t) || attempt >= backoffMs.size) {
                    // exception:exempt a failed audit event is logged and dropped: login is already
                    // open, and bootstrap surfaces any access consequence to the operator.
                    logWarning("session event not delivered after ${attempt + 1} attempt(s)", t)
                    return false
                }
                logWarning("session event attempt ${attempt + 1} failed; retrying", t)
                delay(backoffMs[attempt])
                attempt++
            }
        }
    }

    /** Network/5xx failures are transient; a 401/403 is the server's answer, not a blip. */
    private fun isRetryable(t: Throwable): Boolean =
        t.asBootstrapError() is BootstrapError.ConnectivityFailure

    private fun logWarning(message: String, t: Throwable) {
        runCatching { Log.w(TAG, message, t) }
    }

    companion object {
        private const val TAG = "GoatOSSession"
        /** ~1 minute of retries in total: covers a DNS/network blip without hammering the API. */
        val DEFAULT_BACKOFF_MS: List<Long> = listOf(2_000, 4_000, 8_000, 16_000, 30_000)
    }
}
