package sg.mesha.goatos.capture

import java.util.UUID

/**
 * Recorder tokens are local counters, not analytics identities. Photo and video
 * launchers each own a namespace, renewed on recreation (including process death),
 * while all events for one local request retain the same capture identity.
 */
internal class ProofCameraEventIdentity {
    private val namespace = UUID.randomUUID().toString()

    fun requestId(token: Long): String = "$namespace:$token"
}
