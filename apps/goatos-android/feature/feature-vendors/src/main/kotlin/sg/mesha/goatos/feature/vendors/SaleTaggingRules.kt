package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure functions with no UI and no I/O; exercised by SaleTaggingRulesTest.

/**
 * The client-side half of the tag-only flow's rules (maintainer decision 2026-09-11). Each mirrors
 * a refusal the backend makes anyway; they exist so the refusal is shown against the box that is
 * wrong BEFORE the round trip, not to decide anything the server does not.
 */
object SaleTaggingRules {
    private val weightPattern = Regex("""^\d{1,5}(\.\d{1,2})?$""")
    private val ratePattern = Regex("""^\d{1,10}(\.\d{1,2})?$""")

    /** A weight the backend accepts: kg more than zero, up to two decimals (numeric(7,2)). */
    fun weightLooksValid(raw: String): Boolean {
        val v = raw.trim()
        return weightPattern.matches(v) && (v.toDoubleOrNull() ?: 0.0) > 0.0
    }

    /** A rate the backend accepts: rupees more than zero, up to two decimals (numeric(12,2)). */
    fun rateLooksValid(raw: String): Boolean {
        val v = raw.trim()
        return ratePattern.matches(v) && (v.toDoubleOrNull() ?: 0.0) > 0.0
    }

    /**
     * Whether Submit may be offered. The sale must be filled EXACTLY (the backend's count gate:
     * no half now and half later), every animal must carry a weight and a rate, and nothing in the
     * basket may be one the review refused.
     */
    fun submitGate(basket: List<SaleTaggingBasketAnimalUi>, remaining: Int): SubmitGate = when {
        basket.isEmpty() -> SubmitGate(false, HINT_EMPTY)
        basket.any { it.blockedReason.isNotBlank() } -> SubmitGate(false, HINT_BLOCKED)
        basket.size < remaining -> SubmitGate(false, "${remaining - basket.size} more ${if (remaining - basket.size == 1) "animal" else "animals"} to tag before you can submit")
        basket.size > remaining -> SubmitGate(false, "Remove ${basket.size - remaining}: this sale needs only $remaining more")
        basket.any { !weightLooksValid(it.weight) || !rateLooksValid(it.rate) } -> SubmitGate(false, HINT_FIGURES)
        else -> SubmitGate(true, "")
    }

    data class SubmitGate(val enabled: Boolean, val hint: String)

    /**
     * Picks the ONE animal a typed or scanned tag means, out of what the server's substring search
     * returned. Exact only, on any identifier the animal carries, case-insensitive: a reader hands
     * back the whole number, so a substring hit is a different animal. Null when no single exact
     * match exists -- the caller then shows the matches to choose from.
     */
    fun <T> exactMatch(query: String, matches: List<T>, identifiers: (T) -> List<String>): T? {
        val q = query.trim()
        if (q.isEmpty()) return null
        val exact = matches.filter { m -> identifiers(m).any { it.trim().equals(q, ignoreCase = true) } }
        return exact.singleOrNull()
    }

    const val HINT_EMPTY = "Scan or type a tag to start"
    const val HINT_BLOCKED = "Remove the animals that cannot be sold"
    const val HINT_FIGURES = "Enter a weight and a rate for every animal"
}
