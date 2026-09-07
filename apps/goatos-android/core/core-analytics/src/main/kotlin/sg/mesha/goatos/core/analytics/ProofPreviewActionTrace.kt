package sg.mesha.goatos.core.analytics

data class ProofPreviewActionTrace(
    val action: String,
    val outcome: String,
    val reason: String? = null,
) {
    companion object {
        fun from(raw: String): ProofPreviewActionTrace {
            val parts = raw.split(":", limit = 3)
            val action = parts.getOrNull(0)?.takeIf { it.isNotBlank() } ?: raw
            val outcome = parts.getOrNull(1)?.takeIf { it.isNotBlank() } ?: "preview_action"
            val reason = parts.getOrNull(2)?.takeIf { it.isNotBlank() }
            return ProofPreviewActionTrace(action, outcome, reason)
        }
    }
}
