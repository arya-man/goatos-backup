package sg.mesha.goatos.feature.verify

/**
 * How many proofs of each kind a queue row carries, decided by the backend mime ALONE -- the same
 * rule [VerifyMediaItem.kind] uses to pick a player. A proof whose mime names neither a photo nor a
 * video is counted as a proof of unknown kind rather than guessed into either.
 *
 * A whole-pen weighing item can carry photos beside its videos (an authored SOP slot). Calling
 * every proof a video told the verifier "3 videos" for two videos and a photo (Realme E2E
 * 2026-09-17).
 */
data class VerifyProofCounts(val videos: Int = 0, val photos: Int = 0, val other: Int = 0) {
    val total: Int get() = videos + photos + other

    operator fun plus(that: VerifyProofCounts) =
        VerifyProofCounts(videos + that.videos, photos + that.photos, other + that.other)

    companion object {
        fun ofMimeTypes(mimeTypes: List<String?>): VerifyProofCounts = mimeTypes.fold(VerifyProofCounts()) { counts, mime ->
            val type = mime.orEmpty()
            when {
                type.startsWith("image/", ignoreCase = true) -> counts.copy(photos = counts.photos + 1)
                type.startsWith("video/", ignoreCase = true) -> counts.copy(videos = counts.videos + 1)
                else -> counts.copy(other = counts.other + 1)
            }
        }
    }
}

/** "2 videos · 1 photo" -- each kind the row holds, singular where it is one; blank when none. */
fun verifyProofCountLabel(counts: VerifyProofCounts): String = listOfNotNull(
    counts.videos.takeIf { it > 0 }?.let { if (it == 1) "1 video" else "$it videos" },
    counts.photos.takeIf { it > 0 }?.let { if (it == 1) "1 photo" else "$it photos" },
    counts.other.takeIf { it > 0 }?.let { if (it == 1) "1 proof" else "$it proofs" },
).joinToString(" · ")

/** The pen group's proof total: the sum of its rows' own counts, never a number parsed out of copy. */
fun verifyGroupProofCounts(rows: List<VerificationQueueRow>): VerifyProofCounts =
    rows.fold(VerifyProofCounts()) { sum, row -> sum + row.proofCounts }
