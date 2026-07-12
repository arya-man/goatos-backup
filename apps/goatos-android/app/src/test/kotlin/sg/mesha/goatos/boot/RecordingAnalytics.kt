package sg.mesha.goatos.boot

import sg.mesha.goatos.core.analytics.AnalyticsPort

/** Test double that records every event + user-property write so assertions can inspect them.
 *  Hand-written fake (preferred over a mock) — the port is tiny and this keeps intent obvious. */
class RecordingAnalytics : AnalyticsPort {
    data class Event(val name: String, val props: Map<String, String>)

    val events = mutableListOf<Event>()
    val userProps = linkedMapOf<String, String?>()

    override fun track(event: String, props: Map<String, String>) {
        events += Event(event, props)
    }

    override fun setUserProperty(name: String, value: String?) {
        userProps[name] = value
    }

    fun names(): List<String> = events.map { it.name }
}
