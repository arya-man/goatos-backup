package sg.mesha.goatos.device.feedback

// Haptic/audio operator feedback stays isolated behind this port (TRD: platform
// SDKs live only in device-*). The fake lets features + previews run silently.

interface FeedbackPort {
    fun success()
    fun notDueAlert()
}

class FakeFeedback : FeedbackPort {
    override fun success() {}
    override fun notDueAlert() {}
}
