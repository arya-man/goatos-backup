package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.common.datetime.GoatOsDates
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.feature.vendors.VendorsTone
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale

/**
 * Presentation helpers shared by the Vendors ViewModels (module vendors, maintainer decision
 * 2026-09-03). These format NUMBERS and DATES the farm's way (Indian digit grouping, DD/MM/YYYY);
 * every SENTENCE still comes from the backend and is passed through verbatim.
 */

internal val VENDORS_IST: ZoneId = ZoneId.of("Asia/Kolkata")

internal fun todayIst(): String = LocalDate.now(VENDORS_IST).toString()

/**
 * "2026-09-01" -> "01/09/2026" (every visible date is DD/MM/YYYY, docs/decisions/date-display-format.md);
 * anything else passes through. One helper per surface: this delegates to [GoatOsDates].
 */
internal fun farmDate(iso: String?): String = GoatOsDates.fromWireDate(iso)

/** Indian digit grouping: 785714.5 -> "7,85,714.5"; whole numbers carry no fraction. */
internal fun indianNumber(value: Double, maxFraction: Int = 1): String {
    val negative = value < 0
    val abs = Math.abs(value)
    val rounded = String.format(Locale.US, "%.${maxFraction}f", abs)
    val whole = rounded.substringBefore('.')
    val frac = rounded.substringAfter('.', "").trimEnd('0')
    val grouped = if (whole.length > 3) {
        val head = whole.dropLast(3)
        val tail = whole.takeLast(3)
        val groups = mutableListOf<String>() // mobile-guard:ignore: local digit-grouping scratch list, bounded by the number's length and dropped on return
        var rest = head
        while (rest.length > 2) {
            groups.add(0, rest.takeLast(2))
            rest = rest.dropLast(2)
        }
        if (rest.isNotEmpty()) groups.add(0, rest)
        (groups + tail).joinToString(",")
    } else whole
    val out = if (frac.isEmpty()) grouped else "$grouped.$frac"
    return if (negative) "-$out" else out
}

internal fun rupees(value: Double?): String = if (value == null) "" else "₹" + indianNumber(value, 0)

internal fun kilograms(value: Double?): String = if (value == null) "" else indianNumber(value, 1) + " kg"

internal fun vendorStatusTone(status: String): VendorsTone = when (status) {
    "active" -> VendorsTone.OK
    "negotiating" -> VendorsTone.INFO
    "banned" -> VendorsTone.DANGER
    "inactive" -> VendorsTone.NEUTRAL
    else -> VendorsTone.NEUTRAL
}

internal fun deliveryTone(status: String): VendorsTone = when (status) {
    "reached" -> VendorsTone.OK
    "purchased" -> VendorsTone.WARN
    else -> VendorsTone.NEUTRAL
}

internal fun paymentTone(status: String): VendorsTone = when (status) {
    "Paid" -> VendorsTone.OK
    "Pending" -> VendorsTone.WARN
    else -> VendorsTone.NEUTRAL
}

/** Joins the non-blank parts with a middle dot, the farm's separator on every card. */
internal fun dotJoin(vararg parts: String?): String = parts.filter { !it.isNullOrBlank() }.joinToString(" · ")

/** The register's card subtitle: record type and location, both backend words. */
internal fun VendorDto.typeLine(): String = dotJoin(recordType, locationDisplay)

/** "Bought 01-09-2026 · QA Vendor" */
internal fun FeedPurchaseDto.metaLine(): String = dotJoin(
    if (purchaseDate.isNotBlank()) "Bought ${farmDate(purchaseDate)}" else "",
    vendor,
)

/** Turns a host-relative backend proof route into an absolute URL the player can open. */
internal fun vendorsAbsoluteProofUrl(raw: String): String {
    val trimmed = raw.trim()
    return when {
        trimmed.startsWith("http://") || trimmed.startsWith("https://") -> trimmed
        trimmed.startsWith("/") -> BuildConfig.API_BASE_URL.trimEnd('/') + trimmed
        else -> trimmed
    }
}
