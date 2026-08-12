package sg.mesha.goatos.core.data.capture

fun interface ProofCaptureTelemetry {
    fun track(event: String, props: Map<String, String>)

    object Noop : ProofCaptureTelemetry {
        override fun track(event: String, props: Map<String, String>) = Unit
    }
}
