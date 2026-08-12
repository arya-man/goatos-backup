package sg.mesha.goatos.rfid

import javax.inject.Inject

/**
 * Dev-only fixture transform.
 *
 * The phone-QA fixture reuses the SAME five physical RFID tags in every shed, but goat identity is
 * unique by (tenant, normalized value), so one physical tag cannot belong to eight goats. The
 * fixture therefore stores a per-shed prefix and the dev build applies that prefix on the way in,
 * letting one set of tags stand in for forty distinct animals.
 *
 * Godel 1 carries the raw tags and so has no prefix. Weighing is free-flow and is deliberately NOT
 * transformed: it takes raw RFID in any shed bucket.
 */
class DefaultRfidInputTransform @Inject constructor() : RfidInputTransform {
    override fun vaccination(rawRfid: String, shedId: String): String {
        val value = rawRfid.trim()
        if (value.isBlank()) return rawRfid
        val prefix = phoneFixtureShedPrefixes[shedId] ?: return rawRfid
        return prefix + value
    }

    private companion object {
        val phoneFixtureShedPrefixes = mapOf(
            // Godel 1 (CBE) holds the raw tags, so it is intentionally absent from this map.
            "91000000-0000-4000-8000-000000000203" to "GD2-", // Godel 1 - Part 2 (CBE)
            "9c000000-0000-4000-8000-000000000301" to "G1-", // Gandhi 1 (CBE)
            "9c000000-0000-4000-8000-000000000302" to "G2-", // Gandhi 2 (CBE)
            "91000000-0000-4000-8000-000000000202" to "M2-", // Mandela 2 (CPT)
            "92000000-0000-4000-8000-000000000203" to "C1-", // Castro 1 (CPT)
            "9c000000-0000-4000-8000-000000000303" to "C2-", // Castro 2 (CPT)
            "9c000000-0000-4000-8000-000000000304" to "C3-", // Castro 3 (CPT)
        )
    }
}
