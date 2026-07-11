package sg.mesha.goatos.core.data.cache

/**
 * Deterministic cache-scope key for a read model's JSON-blob-by-scope cache row
 * (docs/decisions/android-offline-first.md). Joins the read's filter args with a delimiter
 * that never appears in a filter value (park/shed/goat ids, ISO dates, cursor tokens, and
 * enum strings are all pipe-free), so distinct filter combinations (park, shed, status,
 * cursor page, ...) land in distinct, tenant/scope-safe rows and a missing (null) arg never
 * collides with a present-but-different one. Bounded in practice: a screen only ever visits
 * the handful of filter combinations its UI actually offers.
 */
internal fun cacheKey(vararg args: String?): String = args.joinToString(separator = "|") { it.orEmpty() }
