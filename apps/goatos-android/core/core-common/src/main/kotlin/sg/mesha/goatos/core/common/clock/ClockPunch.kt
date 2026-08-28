package sg.mesha.goatos.core.common.clock

/**
 * Pure clock-punch decision logic (module clock, maintainer decision 2026-08-27 —
 * docs/features/clock-in-out/plan.md §4.2). Android-free so the block/allow decision and the
 * idempotency-key contract are unit-testable without Robolectric; the collectors that FEED it
 * (Location.isMock, the PackageManager scan, Settings.Global) live in the app module behind
 * `ClockPunchFactsProvider`.
 */

/** One installed app that requests `android.permission.ACCESS_MOCK_LOCATION`. */
data class MockProviderApp(
    val packageName: String,
    /** Human label shown to the person ("Remove *Fake GPS Location* to clock in"). */
    val label: String,
)

/**
 * The punch-time integrity verdict. Computed fresh INSIDE every punch tap — never a one-time
 * install check — so installing a fake-GPS app after a clean first day still blocks the next
 * punch.
 */
data class MockLocationVerdict(
    /** The captured fix itself was mock-provided (`Location.isMock` / `isFromMockProvider`). */
    val mockFix: Boolean,
    /** Installed apps whose requested permissions include ACCESS_MOCK_LOCATION. */
    val mockApps: List<MockProviderApp>,
    /** Recorded, never blocking: blocking dev options would lock out our own dev/test phones. */
    val developerOptions: Boolean,
) {
    /**
     * The client-side hard block: a mock-provided fix OR any installed fake-GPS app refuses the
     * punch outright (the server refuses independently, so a tampered client cannot skip this).
     * Developer options deliberately do NOT block.
     */
    val blocksPunch: Boolean get() = mockFix || mockApps.isNotEmpty()

    /** The offending apps' human labels, for the backend-owned refusal template's %s slot. */
    val appLabels: List<String> get() = mockApps.map { it.label }

    companion object {
        val Clean = MockLocationVerdict(mockFix = false, mockApps = emptyList(), developerOptions = false)
    }
}

/** The two punch directions; `wire` is the path segment / key suffix the contract uses. */
enum class ClockPunchDirection(val wire: String) {
    IN("in"),
    OUT("out"),
}

/**
 * STABLE outbox idempotency key for a punch: `clock:<business_date>:<in|out>`. One in/out pair
 * exists per IST business day, so the DAY is the natural idempotency scope — a retry of the same
 * day's punch replays for free, while tomorrow's punch is a genuinely new act under a new key.
 * Never timestamp-suffixed.
 */
fun clockPunchIdempotencyKey(businessDate: String, direction: ClockPunchDirection): String =
    "clock:$businessDate:${direction.wire}"

/**
 * Outbox GROUP key for a punch: `clock:<business_date>`. The day's in and out share one group so
 * they drain strictly oldest-first (in before out) and never concurrently.
 */
fun clockPunchGroupKey(businessDate: String): String = "clock:$businessDate"
