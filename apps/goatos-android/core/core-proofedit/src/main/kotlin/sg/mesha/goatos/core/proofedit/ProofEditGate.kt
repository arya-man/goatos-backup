package sg.mesha.goatos.core.proofedit

/**
 * Decides whether the trim editor is offered for one capture.
 *
 * TWO conditions, both required, because either alone is unsafe:
 *
 *  1. The backend `feature_flags` entry [FLAG_KEY] is true. The flag is backend-owned like every
 *     other entry in that map, so the editor can be switched off for a tenant without shipping an
 *     APK. ABSENT MEANS OFF — a device that cannot reach bootstrap, or an older backend that has
 *     never heard of this flag, must get today's unedited flow rather than a new one.
 *  2. The capture surface is in [EDITABLE_SURFACES]. Feed packing is the only surface signed off
 *     for editing (maintainer decision): a packing bag is filmed in one continuous stretch where
 *     dead time at the head/tail is normal. Per-animal clinical proof is NOT in this list —
 *     letting an operator cut a vaccination video would let footage of the injection itself be
 *     removed from the evidence a verifier judges.
 *
 * Widening [EDITABLE_SURFACES] is a MAINTAINER decision, never a developer convenience.
 */
object ProofEditGate {

    /** Backend-owned bootstrap `feature_flags` key. Absent or false ⇒ editor never appears. */
    const val FLAG_KEY: String = "proof_video_editing"

    /**
     * Capture surfaces allowed to edit. These are the `feature_surface` values the capture host
     * already reports to analytics, so the gate and the telemetry agree on one vocabulary.
     */
    val EDITABLE_SURFACES: Set<String> = setOf("feed_packing")

    /**
     * @param featureFlags the bootstrap `feature_flags` map, verbatim.
     * @param featureSurface the surface that opened the camera, or null when the host could not
     *   identify one — which fails CLOSED, because an unidentified surface cannot be checked
     *   against the allowlist.
     */
    fun isEditingOffered(featureFlags: Map<String, Boolean>, featureSurface: String?): Boolean {
        if (featureFlags[FLAG_KEY] != true) return false
        val surface = featureSurface?.trim()?.lowercase().orEmpty()
        if (surface.isEmpty()) return false
        return surface in EDITABLE_SURFACES
    }
}
