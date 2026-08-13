package sg.mesha.goatos.capture

/**
 * Test double that runs the REAL production result plumbing, instead of an idealised one.
 *
 * [FakeProofCaptureSource.queueGate] hands every call its OWN `CompletableDeferred`, so a
 * cancelled `await()` abandons that deferred and a late result is discarded for free. Production
 * is not isolated like that: `BindVideoCaptureSource` remembers ONE result relay for the whole
 * composition and every `captureVideo()` call takes its result from it, while the recorder
 * delivers into it asynchronously. Before [ProofCaptureRelay] existed that relay was a single
 * buffered `Channel<CapturedVideo?>` with an untagged `trySend`/`receive` pair, so a cancelled
 * capture left its finished recording in the buffer for the NEXT animal's capture to pick up —
 * a wrong-animal proof that nothing downstream could detect. That is exactly what these tests go
 * red on when the relay is removed.
 *
 * This double therefore delegates to the production [ProofCaptureRelay] rather than reimplementing
 * it: the mechanism under test is the shipped one. [deliverRecorderResult] stands in for CameraX
 * finalizing a recording, with [requestOrdinal] naming which capture request (1-based, in
 * [captureCount] order) that recording was actually shot for — mirroring the recorder composition
 * capturing its own request token in `VideoCaptureLauncher`.
 */
class ChannelBackedProofCaptureSource : ProofCaptureSource {
    private val relay = ProofCaptureRelay()
    private val tokens = mutableListOf<Long>()

    var captureCount: Int = 0
        private set

    override suspend fun captureVideo(captureContext: ProofCaptureContext?): CapturedVideo? {
        val token = relay.nextRequestToken()
        tokens.add(token)
        captureCount += 1
        return relay.awaitResult(token)
    }

    override suspend fun pickVideo(): CapturedVideo? = captureVideo()

    fun deliverRecorderResult(requestOrdinal: Int, video: CapturedVideo?) {
        relay.deliverResult(tokens[requestOrdinal - 1], video)
    }
}
