package sg.mesha.goatos.boot

import android.util.Log
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.AuthSessionEventRequestDto
import sg.mesha.goatos.core.network.CallFailure
import sg.mesha.goatos.core.network.classifyCallFailure
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.random.Random

/** How a background session-event ended. */
sealed interface SessionEventOutcome {
    data object Delivered : SessionEventOutcome
    /** 401/403 -- e.g. `email_not_allowed`. The sign-in must be undone. */
    data class Denied(val statusCode: Int) : SessionEventOutcome
    /** Retries exhausted, a non-retryable failure, or cancelled by logout / a newer sign-in. */
    data object GaveUp : SessionEventOutcome
}

/**
 * Sends the Goat OS `POST /auth/session-events` audit event OFF the login path.
 *
 * Firebase auth is the credential check. The session event is an audit record plus the
 * server-side claim of a pending email grant (`authaudit.claimPendingEmailGrant`); a network
 * failure on it must never block or fail login. On 1.0.40 a brief DNS drop made this call throw
 * `UnknownHostException`, and because login awaited it, a correctly authenticated operator was
 * shown raw exception text and could not get in.
 *
 * So login opens the session first and hands the event here. It runs in the application scope and
 * retries ONLY transient failures (IO, 5xx, 429), honouring Retry-After, with jittered backoff.
 * A 401/403 is reported as [SessionEventOutcome.Denied] so the caller signs the principal out --
 * the server has said this account may not use Goat OS. Other 4xx and serialization errors give up.
 *
 * [cancel] (called by LogoutCoordinator) stops an in-flight retry so a departing user's event can
 * never be posted with the next user's token.
 *
 * Ordering with bootstrap: a first-ever login needs the pending-grant claim before `/app/bootstrap`
 * stops answering 403. [awaitClaimOnce] lets bootstrap wait for the in-flight event ONCE per
 * sign-in, then retry.
 */
@Singleton
class AuthSessionEventSender(
    private val appApi: AppApi,
    private val scope: CoroutineScope,
    private val backoffMs: List<Long> = DEFAULT_BACKOFF_MS,
    /** Extra delay added to each wait; spreads a fleet reconnecting after an outage. */
    private val jitterMs: (Long) -> Long = { base -> Random.nextLong(0, base / 4 + 1) },
) {
    @Inject
    constructor(appApi: AppApi, scope: CoroutineScope) : this(appApi, scope, DEFAULT_BACKOFF_MS)

    private class Attempt(val job: Job, val outcome: CompletableDeferred<SessionEventOutcome>) {
        val claimWaitConsumed = AtomicBoolean(false)
    }

    private val current = AtomicReference<Attempt?>(null)

    /**
     * Starts the event; a newer sign-in supersedes any still-retrying older one. [onDenied] runs in
     * the APPLICATION scope (not the caller's), so a denial still undoes the sign-in after the
     * login screen / Activity is gone. It runs as its own job, after this attempt is cleared, so
     * the logout it performs (which calls [cancel]) cannot cancel itself.
     */
    fun send(
        eventType: String,
        source: String,
        onDenied: suspend (SessionEventOutcome.Denied) -> Unit = {},
    ): Deferred<SessionEventOutcome> {
        val request = AuthSessionEventRequestDto(eventType = eventType, source = source)
        val outcome = CompletableDeferred<SessionEventOutcome>()
        lateinit var attempt: Attempt
        val job = scope.launch(start = CoroutineStart.LAZY) {
            val result = deliverWithRetry(request)
            outcome.complete(result)
            if (result is SessionEventOutcome.Denied && current.compareAndSet(attempt, null)) {
                scope.launch { onDenied(result) }
            }
        }
        job.invokeOnCompletion { outcome.complete(SessionEventOutcome.GaveUp) }
        attempt = Attempt(job, outcome)
        current.getAndSet(attempt)?.job?.cancel()
        job.start()
        return outcome
    }

    /** Stops any in-flight event (logout). Safe to call when nothing is pending. */
    fun cancel() {
        current.getAndSet(null)?.job?.cancel()
    }

    /**
     * For bootstrap's 403: waits up to [timeoutMs] for the current sign-in's event, at most ONCE per
     * sign-in. [onWaiting] fires only when a wait actually starts. Returns true when it was
     * delivered (a bootstrap retry is then worthwhile).
     */
    suspend fun awaitClaimOnce(timeoutMs: Long, onWaiting: () -> Unit = {}): Boolean {
        val attempt = current.get() ?: return false
        if (!attempt.claimWaitConsumed.compareAndSet(false, true)) return false
        if (!attempt.outcome.isCompleted) onWaiting()
        val result = withTimeoutOrNull(timeoutMs) { attempt.outcome.await() }
        return result == SessionEventOutcome.Delivered
    }

    private suspend fun deliverWithRetry(request: AuthSessionEventRequestDto): SessionEventOutcome {
        var attempt = 0
        while (true) {
            try {
                appApi.recordAuthSessionEvent(request)
                return SessionEventOutcome.Delivered
            } catch (c: CancellationException) {
                throw c
            } catch (t: Throwable) {
                when (val failure = t.classifyCallFailure()) {
                    is CallFailure.Denied -> {
                        logWarning("session event denied (${failure.statusCode})", t)
                        return SessionEventOutcome.Denied(failure.statusCode)
                    }
                    CallFailure.Permanent -> {
                        // exception:exempt a non-retryable audit failure is logged and dropped: the
                        // session is already open and bootstrap surfaces any access consequence.
                        logWarning("session event failed permanently", t)
                        return SessionEventOutcome.GaveUp
                    }
                    is CallFailure.Transient -> {
                        if (attempt >= backoffMs.size) {
                            logWarning("session event not delivered after ${attempt + 1} attempt(s)", t)
                            return SessionEventOutcome.GaveUp
                        }
                        val base = (failure.retryAfterMs ?: backoffMs[attempt]).coerceAtMost(MAX_WAIT_MS)
                        logWarning("session event attempt ${attempt + 1} failed; retrying in ${base}ms", t)
                        delay(base + jitterMs(base))
                        attempt++
                    }
                }
            }
        }
    }

    private fun logWarning(message: String, t: Throwable) {
        runCatching { Log.w(TAG, message, t) }
    }

    companion object {
        private const val TAG = "GoatOSSession"
        private const val MAX_WAIT_MS = 120_000L
        /** ~1 minute of retries in total: covers a DNS/network blip without hammering the API. */
        val DEFAULT_BACKOFF_MS: List<Long> = listOf(2_000, 4_000, 8_000, 16_000, 30_000)
    }
}
