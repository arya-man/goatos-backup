package sg.mesha.goatos.viewmodel

/**
 * The verification status a worklist row RENDERS, given the backend row and whether this phone has
 * a submit for that grain still alive in the outbox.
 *
 * ONE function for every verifier-gated flow. Each flow differs only in the token its own
 * vocabulary uses for "waiting on a verifier" ([inReviewToken]) — Feed Transport says
 * "verification_due", everything else says "pending_verification" — so a new flow supplies a string,
 * not a new copy of this logic. Four near-identical copies of it existed before, and they drifted.
 *
 * Precedence, in order:
 *   a. backend [completedToken] -> keep it; server acceptance always overrides a local hint.
 *   b. non-blank [reworkReason] -> keep the backend status. A reworked line comes back as "pending"
 *      WITH a reason, so overlaying it would hide the rejection the operator must act on.
 *   c. still queued locally -> [inReviewToken].
 *   d. otherwise -> the backend status.
 */
internal fun overlayVerificationStatus(
    backendStatus: String,
    reworkReason: String?,
    isLocallySubmitted: Boolean,
    inReviewToken: String,
    completedToken: String = "completed",
): String = when {
    backendStatus == completedToken -> completedToken
    !reworkReason.isNullOrBlank() -> backendStatus
    isLocallySubmitted -> inReviewToken
    else -> backendStatus
}

/** What "waiting on a verifier" is called in each flow's own vocabulary. */
internal const val IN_REVIEW_PENDING_VERIFICATION = "pending_verification"
internal const val IN_REVIEW_VERIFICATION_DUE = "verification_due"
